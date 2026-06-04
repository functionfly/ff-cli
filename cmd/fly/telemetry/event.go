// Package telemetry defines the minimal production telemetry surface for
// the CLI and local runtime. It is intentionally dependency-free so that
// operators can back it with logrus, OpenTelemetry, or a no-op sink.
package telemetry

import "time"

// EventKind classifies lifecycle events emitted by the CLI and runtime.
type EventKind string

const (
	EventDeployStart    EventKind = "deploy_start"
	EventDeployEnd      EventKind = "deploy_end"
	EventLocalExecStart EventKind = "local_exec_start"
	EventLocalExecEnd   EventKind = "deploy_end"
	EventBuildStart     EventKind = "build_start"
	EventBuildEnd       EventKind = "build_end"
)

// Event is a structured telemetry record with minimal production fields.
type Event struct {
	Kind   EventKind
	Status string // "ok", "error"
	Error  string
	Start  time.Time
}

// DurationMs returns the elapsed milliseconds since Start.
func (e Event) DurationMs() int64 {
	if e.Start.IsZero() {
		return 0
	}
	return int64(time.Since(e.Start) / time.Millisecond)
}
