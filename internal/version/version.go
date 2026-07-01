// Package version holds build information injected at compile time via ldflags.
// This is used by the CLI to display version information.
package version

import (
	"fmt"
	"time"
)

// Version is the semantic version string, injected at build time.
// Defaults to "dev" if not set during build.
var Version = "1.1.0"

// Commit is the git commit hash, injected at build time.
// Defaults to "" if not set during build.
var Commit = ""

// Date is the build timestamp, injected at build time.
// Defaults to "" if not set during build.
var Date = ""

// BuildDate parses the Date string into a time.Time.
// Returns zero time if Date is empty or invalid.
func BuildDate() time.Time {
	if Date == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, Date)
	if err != nil {
		return time.Time{}
	}
	return t
}

// Info returns a formatted string with version information.
func Info() string {
	return fmt.Sprintf("ff version %s (commit: %s, date: %s)", Version, Commit, Date)
}

// Short returns just the version string.
func Short() string {
	if Version == "dev" {
		return "dev"
	}
	return Version
}
