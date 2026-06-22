// Package version exposes the software version string and related build
// metadata used to label the running build. The values are intentionally
// internal — users cannot override them via homepage.yaml, environment
// variables, or runtime configuration.
//
// The variables below are mutable (not const) so they can be overridden at
// build time via -ldflags. The Makefile (and the multi-arch Docker workflow)
// injects four values from the surrounding toolchain:
//
//	go build -ldflags "\
//	  -s -w \
//	  -X github.com/bassista/go_spin/internal/version.Version=$(git describe ...) \
//	  -X github.com/bassista/go_spin/internal/version.BuildTime=$(date ...) \
//	  -X github.com/bassista/go_spin/internal/version.GitCommit=$(git rev-parse ...) \
//	  -X github.com/bassista/go_spin/internal/version.GoVersion=$(go version ...)"
//
// At runtime the values flow into the homepage JSON response (top-level
// fields, never under "settings" — see internal/api/controller/homepage_controller.go)
// and are rendered in the homepage footer.
package version

// Version is the software version reported by the server. It is rendered in
// the homepage footer (bottom-right) as part of the JSON response's top-level
// "version" field and surfaced to the UI.
//
// At build time this is normally populated by `git describe --tags --always --dirty`
// via the Makefile (e.g. "v1.2.3", "v1.2.3-4-gabc1234", or "v1.2.3-dirty").
var Version = "1.0.0"

// BuildTime is the UTC timestamp at which the binary was produced. Populated
// at build time from `date -u '+%Y-%m-%d_%H:%M:%S'`. Useful for telling two
// builds of the same commit apart and for correlating a deployed artifact
// with CI logs.
var BuildTime = "unknown"

// GitCommit is the short SHA of the commit the binary was built from.
// Populated at build time from `git rev-parse --short HEAD`.
var GitCommit = "unknown"

// GoVersion is the Go toolchain version the binary was built with (e.g.
// "go1.25.6"). Populated at build time from `go version`.
var GoVersion = "unknown"

// BuildInfo returns a human-readable, single-line summary of the build
// metadata. Kept stable (no newline, no trailing whitespace) so it can be
// embedded verbatim in the homepage footer tooltip and the startup log.
//
// Dev runs (no -ldflags passed) leave GitCommit/BuildTime/GoVersion at their
// "unknown" defaults; those segments are skipped here so the output does
// not render four literal "unknown" tokens.
func BuildInfo() string {
	s := Version
	if GitCommit != "" && GitCommit != "unknown" {
		s += " (" + GitCommit + ")"
	}
	if BuildTime != "" && BuildTime != "unknown" {
		s += " built " + BuildTime + " UTC"
	}
	if GoVersion != "" && GoVersion != "unknown" {
		s += " with " + GoVersion
	}
	return s
}
