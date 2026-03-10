package tracer

import (
	"maps"
	"time"
)

// EventType represents the kind of trace event.
type EventType int

const (
	EventSpanStart EventType = iota
	EventSpanEnd
)

func (e EventType) String() string {
	switch e {
	case EventSpanStart:
		return "span_start"
	case EventSpanEnd:
		return "span_end"
	default:
		return "unknown"
	}
}

// Event is the unit of data sent from the instrumented application
// to the go-trace collector over a Unix domain socket.
type Event struct {
	Type     EventType         `json:"type"`
	SpanID   string            `json:"span_id"`
	TraceID  string            `json:"trace_id"`
	ParentID string            `json:"parent_id,omitzero"`
	Name     string            `json:"name"`
	Kind     SpanKind          `json:"kind"`
	Time     time.Time         `json:"time"`
	Attrs    map[string]string `json:"attrs,omitzero"`
}

// NewSpanStartEvent creates an event representing the beginning of a span.
func NewSpanStartEvent(spanID, traceID, parentID, name string, kind SpanKind, t time.Time) Event {
	return Event{
		Type:     EventSpanStart,
		SpanID:   spanID,
		TraceID:  traceID,
		ParentID: parentID,
		Name:     name,
		Kind:     kind,
		Time:     t,
	}
}

// NewSpanEndEvent creates an event representing the end of a span.
func NewSpanEndEvent(spanID, traceID string, t time.Time, attrs map[string]string) Event {
	var cloned map[string]string
	if len(attrs) > 0 {
		cloned = make(map[string]string, len(attrs))
		maps.Copy(cloned, attrs)
	}
	return Event{
		Type:    EventSpanEnd,
		SpanID:  spanID,
		TraceID: traceID,
		Time:    t,
		Attrs:   cloned,
	}
}
