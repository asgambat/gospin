// Package version exposes the software version string used to label the
// running build. The value is intentionally internal — users cannot override
// it via homepage.yaml, environment variables, or runtime configuration.
//
// The Version variable is mutable (not const) so it can be overridden at
// build time via -ldflags:
//
//	go build -ldflags "-X github.com/bassista/go_spin/internal/version.Version=1.2.3" ./cmd/server
package version

// Version is the software version reported by the server. It is rendered in
// the homepage footer (bottom-right) as part of the JSON response's top-level
// "version" field and surfaced to the UI.
var Version = "1.0.0"
