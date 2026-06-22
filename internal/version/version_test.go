package version

import (
	"strings"
	"testing"
)

// setVars overrides the four package-level build-metadata vars for the
// duration of a test, restoring them through t.Cleanup so subsequent tests
// are not affected by side effects. Tests must call this BEFORE touching the
// vars so the cleanup hook is registered even when assertions later fail.
func setVars(t *testing.T, version, buildTime, gitCommit, goVersion string) {
	t.Helper()
	origVersion, origBuildTime, origGitCommit, origGoVersion := Version, BuildTime, GitCommit, GoVersion
	Version = version
	BuildTime = buildTime
	GitCommit = gitCommit
	GoVersion = goVersion
	t.Cleanup(func() {
		Version = origVersion
		BuildTime = origBuildTime
		GitCommit = origGitCommit
		GoVersion = origGoVersion
	})
}

// TestBuildInfo_NoUnknownWhenAllVarsSet asserts that when every metadata var
// is populated with a real value (no "unknown" placeholder, no empty string),
// BuildInfo() renders the full string without leaking the literal "unknown"
// substring anywhere (which would otherwise show up in UI footers and
// container logs).
func TestBuildInfo_NoUnknownWhenAllVarsSet(t *testing.T) {
	setVars(t, "v1.2.3", "2026-06-22 14:30:00", "abc1234", "go1.25.6")

	got := BuildInfo()
	if strings.Contains(got, "unknown") {
		t.Errorf("BuildInfo() = %q; want output without any \"unknown\" placeholder when all vars are populated", got)
	}
	for _, want := range []string{"v1.2.3", "2026-06-22 14:30:00", "abc1234", "go1.25.6"} {
		if !strings.Contains(got, want) {
			t.Errorf("BuildInfo() = %q; missing expected segment %q", got, want)
		}
	}
}

// TestBuildInfo_SkipsEmptyAndUnknown covers the contract that each
// suffix segment — GitCommit in parentheses, BuildTime with UTC suffix,
// GoVersion with " with " prefix — is omitted when its underlying var is
// either the empty string or the literal "unknown" placeholder. The
// table-driven cases keep the contract readable and make adding new cases
// a one-liner.
//
// Note: Go 1.22+ (this module is on Go 1.25.6) gives every loop iteration
// its own `tc` scope, so the explicit `tc := tc` capture is no longer
// needed and is intentionally omitted.
func TestBuildInfo_SkipsEmptyAndUnknown(t *testing.T) {
	cases := []struct {
		name           string
		version        string
		buildTime      string
		gitCommit      string
		goVersion      string
		mustContain    []string
		mustNotContain []string
	}{
		{
			// Dev / preview scenario: vars still at their "unknown" placeholders.
			// Helper should render just `1.0.0` — no clutter, no leaked placeholder.
			name:           "dev-run-all-defaults",
			version:        "1.0.0",
			buildTime:      "unknown",
			gitCommit:      "unknown",
			goVersion:      "unknown",
			mustContain:    []string{"1.0.0"},
			mustNotContain: []string{"unknown", "(", "built ", " with "},
		},
		{
			// Empty-string form (degenerate placeholder, e.g. cleared env var)
			// must behave identically to the literal "unknown" placeholder.
			name:           "all-empty-strings",
			version:        "1.0.0",
			buildTime:      "",
			gitCommit:      "",
			goVersion:      "",
			mustContain:    []string{"1.0.0"},
			mustNotContain: []string{"unknown", "(", "built ", " with ", " ()"},
		},
		{
			// Only GitCommit is set — emit `v1.2.3 (abc1234)` with no
			// build-time / Go-version noise.
			name:           "only-commit-set",
			version:        "v1.2.3",
			buildTime:      "unknown",
			gitCommit:      "abc1234",
			goVersion:      "unknown",
			mustContain:    []string{"v1.2.3", "(abc1234)"},
			mustNotContain: []string{"unknown", "built ", " with "},
		},
		{
			// GitCommit missing, the other two present — emit
			// `v1.2.3 built 2026-06-22 14:30:00 UTC with go1.25.6`.
			// mustNotContain uses `(abc1234)` and `()` (specific enough to
			// not false-positive on any future legitimate paren) rather than
			// a bare `(`.
			name:           "build-time-and-go-version-only",
			version:        "v1.2.3",
			buildTime:      "2026-06-22 14:30:00",
			gitCommit:      "unknown",
			goVersion:      "go1.25.6",
			mustContain:    []string{"v1.2.3", "2026-06-22 14:30:00", "UTC", "go1.25.6"},
			mustNotContain: []string{"unknown", "(abc1234)", "()"},
		},
		{
			// Mixed: GitCommit empty (skip), BuildTime set, GoVersion empty (skip).
			// Guards against accidentally treating "" and "unknown" differently.
			name:           "mixed-empty-and-set",
			version:        "v1.2.3",
			buildTime:      "2026-06-22 14:30:00",
			gitCommit:      "",
			goVersion:      "",
			mustContain:    []string{"v1.2.3", "2026-06-22 14:30:00", "UTC"},
			mustNotContain: []string{"unknown", "()", "(abc1234)", " with "},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setVars(t, tc.version, tc.buildTime, tc.gitCommit, tc.goVersion)

			got := BuildInfo()
			for _, want := range tc.mustContain {
				if !strings.Contains(got, want) {
					t.Errorf("BuildInfo() = %q; missing expected substring %q", got, want)
				}
			}
			for _, bad := range tc.mustNotContain {
				if strings.Contains(got, bad) {
					t.Errorf("BuildInfo() = %q; should not contain %q", got, bad)
				}
			}
		})
	}
}

// TestBuildInfo_StableWithoutLDFlags guards against accidental
// non-determinism in BuildInfo() — e.g. someone adding a time.Now() call
// inside the helper that would produce a different string on every
// invocation and pollute container logs with ever-changing build-time
// values. Two consecutive calls with no ldflags overlay must produce
// byte-identical output.
func TestBuildInfo_StableWithoutLDFlags(t *testing.T) {
	// Pin known values so the assertion is robust to any test run order.
	setVars(t, "v1.0.0", "2026-06-22 14:30:00", "abc1234", "go1.25.6")

	first := BuildInfo()
	second := BuildInfo()
	if first != second {
		t.Errorf("BuildInfo() is not stable across consecutive invocations: first=%q second=%q", first, second)
	}
}

// TestBuildInfo_VersionIsNotGated pins a deliberate asymmetry in the
// helper: BuildTime / GitCommit / GoVersion are gated against the
// "unknown" placeholder and the empty string, while Version is rendered
// verbatim because every release carries a sensible default ("1.0.0") or
// whatever `-X -ldflags` injected. Today the asymmetry is harmless; this
// test ensures a future refactor doesn't silently gate Version too, which
// would break the ldflags-driven release flow.
func TestBuildInfo_VersionIsNotGated(t *testing.T) {
	t.Run("version-rendered-when-unknown", func(t *testing.T) {
		setVars(t, "unknown", "unknown", "unknown", "unknown")
		if got := BuildInfo(); got != "unknown" {
			t.Errorf("BuildInfo() with every var set to %q = %q; want %q (Version must render verbatim, others must be skipped)", "unknown", got, "unknown")
		}
	})

	t.Run("version-rendered-when-empty", func(t *testing.T) {
		// Pin all four positions to "" (not just Version) so the exact-equality
		// assertion is independent of any earlier test's residual state, even
		// with `go test -shuffle`. The output is exactly "" because every
		// gated segment is skipped and Version renders verbatim.
		setVars(t, "", "", "", "")
		if got := BuildInfo(); got != "" {
			t.Errorf("BuildInfo() with every var set to %q = %q; want %q (Version is unconditional, all gated segments skip)", "", got, "")
		}
	})
}
