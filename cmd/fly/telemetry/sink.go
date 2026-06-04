package telemetry

import "context"

// Sink receives telemetry events.
type Sink interface {
	Emit(ctx context.Context, event Event)
}

// noopSink is the default and drops every event.
type noopSink struct{}

func (noopSink) Emit(_ context.Context, _ Event) {}

var defaultSink Sink = noopSink{}

// SetSink replaces the global sink. It is not safe for concurrent use;
// call it during CLI initialization before any workers start.
func SetSink(s Sink) {
	if s == nil {
		s = noopSink{}
	}
	defaultSink = s
}

// Emit sends an event to the configured sink without blocking callers for
// long. The no-op sink returns immediately.
func Emit(ctx context.Context, event Event) {
	defaultSink.Emit(ctx, event)
}
