package scheduler

import (
	"context"
	"sync"
	"time"

	"github.com/asgambat/gospin/internal/cache"
	"github.com/asgambat/gospin/internal/logger"
	"github.com/asgambat/gospin/internal/repository"
	"github.com/asgambat/gospin/internal/runtime"
)

type DayFlags struct {
	StartedDayKey string
	StoppedDayKey string
}

// PollingScheduler evaluates schedules on a fixed interval and performs at most
// one start and one stop action per container per day (in the configured timezone).
//
// Semantics:
//   - A timer may define both StartTime and StopTime, only StartTime (start-only: the
//     target is brought up at that time and left running) or only StopTime
//     (stop-only: the target is stopped at that time even if this scheduler did not
//     start it).
//   - A start+stop window containing the current time keeps the target up, including
//     against a stop-only timer that is due today.
//   - A start-only timer keeps the target up until midnight unless a stop-only timer
//     for the same target is due today: the explicit stop wins.
//   - If StartedDayKey == today, start is never attempted again today, regardless of running state.
//   - If StoppedDayKey == today, stop is never attempted again today.
//   - Stop evaluation is only performed after a start evaluation has happened that
//     day, unless a stop-only timer is due.
//
// NOTE: Flags are in-memory only.
type PollingScheduler struct {
	store   cache.ReadOnlyStore
	runtime runtime.ContainerRuntime
	loc     *time.Location
	flags   map[string]DayFlags
	poll    time.Duration
	mu      sync.Mutex
}

func NewPollingScheduler(store cache.ReadOnlyStore, rt runtime.ContainerRuntime, poll time.Duration, loc *time.Location) *PollingScheduler {
	if loc == nil {
		loc = time.Local
	}

	return &PollingScheduler{
		store:   store,
		runtime: rt,
		poll:    poll,
		loc:     loc,
		flags:   map[string]DayFlags{},
	}
}

func (s *PollingScheduler) Start(ctx context.Context) {
	logger.WithComponent("sched").Debugf("starting polling scheduler with interval: %v, timezone: %s", s.poll, s.loc.String())
	ticker := time.NewTicker(s.poll)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				logger.WithComponent("sched").Info("scheduler stopped")
				return
			case <-ticker.C:
				s.tick(ctx)
			}
		}
	}()
}

func (s *PollingScheduler) tick(ctx context.Context) {
	logger.WithComponent("sched").Debugf("polling scheduler tick started")
	doc, err := s.store.Snapshot()
	if err != nil {
		logger.WithComponent("sched").Errorf("snapshot error: %v", err)
		return
	}

	now := time.Now().In(s.loc)
	todayKey := dayKey(now)
	logger.WithComponent("sched").Debugf("evaluating schedules for today: %s, current time: %s", todayKey, now.Format("15:04:05"))

	// Build lookup maps for efficient access during schedule evaluation.
	containersByName := map[string]repository.Container{}
	for _, c := range doc.Containers {
		if c.Name == "" {
			continue
		}
		containersByName[c.Name] = c
	}

	groupsByName := map[string]repository.Group{}
	for _, g := range doc.Groups {
		if g.Name == "" {
			continue
		}
		groupsByName[g.Name] = g
	}

	// Containers a start+stop window says must be running right now. These windows
	// declare their own end, so they win over a stop-only request.
	windowRunning := map[string]bool{}
	// Containers kept up by a start-only timer, which leaves them running until
	// midnight. A stop-only timer that is due today overrides this.
	openEndedRunning := map[string]bool{}
	// Containers a due stop-only timer asks to stop today. It overrides
	// openEndedRunning but not an active windowRunning.
	stopRequested := map[string]bool{}

	// Evaluate all schedules to determine which containers should be running based on active timers.
	for _, sched := range doc.Schedules {
		// Expand the schedule target into a list of container names (handles both "container" and "group" target types).
		containerNames := expandScheduleTargets(sched, containersByName, groupsByName)
		if len(containerNames) == 0 {
			logger.WithComponent("sched").Debugf("schedule %s expanded to 0 containers", sched.ID)
			continue
		}

		logger.WithComponent("sched").Tracef("schedule %s (target: %s) expanded to %d containers", sched.ID, sched.Target, len(containerNames))
		for _, timer := range sched.Timers {
			if timer.Active != nil && !*timer.Active {
				logger.WithComponent("sched").Debugf("timer inactive for schedule %s", sched.ID)
				continue
			}
			hasStart := timer.StartTime != ""
			hasStop := timer.StopTime != ""

			// Start side: a start+stop timer runs inside its window, a start-only
			// timer runs from its start time until the rest of the day.
			if hasStart {
				active := isStartOnlyActiveNow(timer, now)
				if hasStop {
					active = isTimerActiveNow(timer, now)
				}
				if active {
					logger.WithComponent("sched").Debugf("timer %s is active for schedule %s, marking %d containers as running", timerLabel(timer), sched.ID, len(containerNames))
					// For each container targeted by this schedule, mark it as desired running if the container itself is active.
					for _, containerName := range containerNames {
						c, ok := containersByName[containerName]
						if !ok {
							continue
						}
						// Respect the container's own active flag.
						if c.Active != nil && !*c.Active {
							continue
						}
						if hasStop {
							windowRunning[containerName] = true
							continue
						}
						openEndedRunning[containerName] = true
					}
				}
			}

			// Stop side: a stop-only timer asks for a stop once its stop time is due,
			// regardless of whether this scheduler started the container today.
			if hasStop && !hasStart && isStopDueNow(timer, now) {
				logger.WithComponent("sched").Debugf("timer %s is due for schedule %s, requesting stop of %d containers", timerLabel(timer), sched.ID, len(containerNames))
				for _, containerName := range containerNames {
					c, ok := containersByName[containerName]
					if !ok {
						continue
					}
					if c.Active != nil && !*c.Active {
						continue
					}
					stopRequested[containerName] = true
				}
			}
		}
	}

	// For each container, decide whether to start or stop based on desired state and day-key flags.
	for containerName := range containersByName {
		// Check for context cancellation to allow early exit during long iterations
		select {
		case <-ctx.Done():
			logger.WithComponent("sched").Debugf("tick cancelled, exiting container loop")
			return
		default:
		}

		flags := s.getFlags(containerName)
		// A window timer that contains the current time wins over a due stop-only
		// timer; an open-ended start-only timer does not, so an explicit stop can end
		// the "left running until midnight" state it created.
		shouldRun := windowRunning[containerName] ||
			(openEndedRunning[containerName] && !stopRequested[containerName])
		logger.WithComponent("sched").Debugf("container %s: shouldRun=%v, stopRequested=%v, startedToday=%v, stoppedToday=%v",
			containerName, shouldRun, stopRequested[containerName], flags.StartedDayKey == todayKey, flags.StoppedDayKey == todayKey)
		// If we already attempted to start this container today, skip to avoid repeated attempts.
		// This enforces "at most one start per day" even if the container stops later.
		if shouldRun {
			if flags.StartedDayKey == todayKey {
				logger.WithComponent("sched").Debugf("container %s already started today, skipping", containerName)
				continue
			}
			// Check current runtime state.
			running, err := s.runtime.IsRunning(ctx, containerName)
			if err != nil {
				logger.WithComponent("sched").Errorf("IsRunning(%s) error: %v", containerName, err)
				continue
			}
			if !running {
				if err := s.runtime.Start(ctx, containerName); err != nil {
					logger.WithComponent("sched").Errorf("Start(%s) error: %v", containerName, err)
					continue
				}
				logger.WithComponent("sched").Infof("started %s", containerName)
			}
			// Mark that a start attempt was made today (even if it was already running).
			flags.StartedDayKey = todayKey
			s.setFlags(containerName, flags)
			continue
		}

		// Container should not be running now.
		// Stop evaluation happens when a stop-only timer is due today, or when a
		// start evaluation already occurred today (to avoid premature stops).
		if !stopRequested[containerName] && flags.StartedDayKey != todayKey {
			logger.WithComponent("sched").Tracef("container %s not started today, skipping stop evaluation", containerName)
			continue
		}
		// If we already attempted to stop this container today, skip.
		if flags.StoppedDayKey == todayKey {
			logger.WithComponent("sched").Debugf("container %s already stopped today, skipping", containerName)
			continue
		}

		running, err := s.runtime.IsRunning(ctx, containerName)
		if err != nil {
			logger.WithComponent("sched").Errorf("IsRunning(%s) error: %v", containerName, err)
			continue
		}
		if running {
			if err := s.runtime.Stop(ctx, containerName); err != nil {
				logger.WithComponent("sched").Errorf("Stop(%s) error: %v", containerName, err)
				continue
			}
			logger.WithComponent("sched").Infof("stopped %s", containerName)
		}
		// Mark that a stop attempt was made today (even if it was already stopped).
		flags.StoppedDayKey = todayKey
		s.setFlags(containerName, flags)
	}
	logger.WithComponent("sched").Debugf("polling scheduler tick completed")
}

func (s *PollingScheduler) getFlags(containerName string) DayFlags {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.flags[containerName]
}

func (s *PollingScheduler) setFlags(containerName string, flags DayFlags) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flags[containerName] = flags
}

func dayKey(t time.Time) string {
	return t.Format("2006-01-02")
}

func expandScheduleTargets(
	sched repository.Schedule,
	containersByName map[string]repository.Container,
	groupsByName map[string]repository.Group,
) []string {
	if sched.Target == "" {
		return nil
	}

	switch sched.TargetType {
	case "container":
		if _, ok := containersByName[sched.Target]; !ok {
			return nil
		}
		return []string{sched.Target}
	case "group":
		g, ok := groupsByName[sched.Target]
		if !ok {
			return nil
		}
		if g.Active != nil && !*g.Active {
			return nil
		}
		out := make([]string, 0, len(g.Container))
		for _, name := range g.Container {
			if name == "" {
				continue
			}
			out = append(out, name)
		}
		return out
	default:
		return nil
	}
}

// timeOfDayLayout is the HH:MM layout used by timer start/stop times in the
// schedule payloads and in config/data/config.json.
const timeOfDayLayout = "15:04"

// timerLabel renders a timer for log messages and covers start-only and
// stop-only timers, where one of the two times is empty.
func timerLabel(timer repository.Timer) string {
	switch {
	case timer.StartTime != "" && timer.StopTime != "":
		return timer.StartTime + "-" + timer.StopTime
	case timer.StartTime != "":
		return "start " + timer.StartTime
	default:
		return "stop " + timer.StopTime
	}
}

// isStartOnlyActiveNow reports whether a start-only timer is active: the weekday
// matches and the start time has already passed today. The container stays
// desired-running until midnight; what happens after that is decided by other
// schedules or by manual actions.
func isStartOnlyActiveNow(timer repository.Timer, now time.Time) bool {
	if timer.StartTime == "" || !containsInt(timer.Days, int(now.Weekday())) {
		return false
	}
	startClock, err := time.Parse(timeOfDayLayout, timer.StartTime)
	if err != nil {
		return false
	}
	start := time.Date(now.Year(), now.Month(), now.Day(), startClock.Hour(), startClock.Minute(), 0, 0, now.Location())
	return !now.Before(start)
}

// isStopDueNow reports whether a stop-only timer must stop its targets today:
// the weekday matches and the stop time has already passed.
func isStopDueNow(timer repository.Timer, now time.Time) bool {
	if timer.StopTime == "" || !containsInt(timer.Days, int(now.Weekday())) {
		return false
	}
	stopClock, err := time.Parse(timeOfDayLayout, timer.StopTime)
	if err != nil {
		return false
	}
	stop := time.Date(now.Year(), now.Month(), now.Day(), stopClock.Hour(), stopClock.Minute(), 0, 0, now.Location())
	return !now.Before(stop)
}

func isTimerActiveNow(timer repository.Timer, now time.Time) bool {
	startClock, err := time.Parse(timeOfDayLayout, timer.StartTime)
	if err != nil {
		return false
	}
	stopClock, err := time.Parse(timeOfDayLayout, timer.StopTime)
	if err != nil {
		return false
	}

	// Check windows anchored to today and yesterday (handles cross-midnight).
	for _, dayOffset := range []int{0, -1} {
		base := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, dayOffset)

		weekday := int(base.Weekday())
		if !containsInt(timer.Days, weekday) {
			continue
		}

		start := time.Date(base.Year(), base.Month(), base.Day(), startClock.Hour(), startClock.Minute(), 0, 0, now.Location())
		stop := time.Date(base.Year(), base.Month(), base.Day(), stopClock.Hour(), stopClock.Minute(), 0, 0, now.Location())
		if !stop.After(start) {
			stop = stop.Add(24 * time.Hour)
		}

		if (now.Equal(start) || now.After(start)) && now.Before(stop) {
			return true
		}
	}

	return false
}

func containsInt(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
