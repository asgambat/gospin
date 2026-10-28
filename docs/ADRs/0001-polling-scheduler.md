---
title: "ADR-0001: Polling scheduler for start/stop windows"
status: "Accepted"
date: "2026-10-28"
authors: "Maintainers"
tags: ["architecture", "scheduler", "decision"]
supersedes: ""
superseded_by: ""
---

# ADR-0001: Polling scheduler for start/stop windows

## Status

**Accepted**

## Context

GoSpin manages containers from schedules made of timers, and each timer can define a start time, a
stop time or both, on selected weekdays, evaluated in a configured timezone
(`misc.scheduling_timezone`). The service runs as a single binary, often on a home server, and must
work without cron, systemd timers or any external orchestrator. Schedules are edited in the UI and
persisted in the JSON data file, so the engine has to pick up changes without a restart.

## Decision

A single goroutine ticks on `data.scheduling_poll_interval_secs` and, on every tick, takes a snapshot
of the store and evaluates every active schedule and timer against the current time in the configured
timezone (`internal/scheduler/polling_scheduler.go`).

The engine keeps in-memory per-container day flags (`StartedDayKey`, `StoppedDayKey`) so that it
performs at most one start and one stop per container per day:

- a start+stop window containing the current time keeps the target up, even against a stop-only timer
  due the same day;
- a start-only timer keeps the target up until midnight unless a stop-only timer for the same target
  is due today, in which case the explicit stop wins;
- stop evaluation only happens after a start evaluation that day, unless a stop-only timer is due.

The flags are deliberately in-memory: a restart re-evaluates the current state rather than replaying
history.

## Consequences

### Positive

- **POS-001**: One loop and one state map; no scheduler library, no queue and no persistence of
  scheduling state, which keeps the single-binary deployment honest.
- **POS-002**: Configuration changes are picked up on the next tick, because every tick re-reads the
  store snapshot instead of caching the schedules.
- **POS-003**: Evaluation is a pure function of (time, schedule, day flags), which makes the behaviour
  easy to test with an injected `time.Location` and a fake runtime.

### Negative

- **NEG-001**: Precision is bounded by the poll interval (30 seconds by default): a timer fires
  within one tick of its configured minute, not exactly at it.
- **NEG-002**: A restart loses the day flags, so a container that was already started or stopped today
  can be acted on once more.
- **NEG-003**: The engine acts only while the process runs; a long outage is not replayed.

## Alternatives Considered

### Cron expression engine

- **ALT-001**: **Description**: Parse cron expressions and let each schedule carry its own triggers.
- **ALT-002**: **Rejection Reason**: Cron expresses instants, while GoSpin schedules express windows
  (start and stop with an implicit "keep it running in between"). Mapping windows onto cron entries
  produces pairs of entries that can drift apart, and the day-level idempotency would still have to
  live somewhere else.

### Per-timer goroutines with `time.AfterFunc`

- **ALT-003**: **Description**: Create a timer per schedule and reset it when the configuration
  changes.
- **ALT-004**: **Rejection Reason**: Every configuration reload would have to diff and rebuild the
  timer set correctly; a bug there shows up as a missed start, while a poll tick is self-correcting.

### External cron or systemd timers

- **ALT-005**: **Description**: Expose the schedules as generated crontab or unit files.
- **ALT-006**: **Rejection Reason**: Requires root or host configuration outside the container, which
  breaks the promise that GoSpin manages itself from its own UI and configuration file.

## Implementation Notes

- **IMP-001**: The scheduler receives the store, the runtime, the poll interval and the timezone from
  `internal/app`; it never reads configuration on its own.
- **IMP-002**: Flags live in a map guarded by a mutex, and the tick logs at debug level so the
  decision for every container is traceable without flooding the log.
- **IMP-003**: Success is observable through the container running state and the scheduler log lines;
  there is no metrics endpoint dedicated to the scheduler.

## References

- **REF-001**: [ARCHITECTURE.md - Scheduler Architecture](../ARCHITECTURE.md#scheduler-architecture)
- **REF-002**: [README.md - Schedules](../../README.md#schedules)
- **REF-003**: `internal/scheduler/polling_scheduler.go`
