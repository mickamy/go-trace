package tracer_test

import (
	"testing"
	"time"

	"github.com/mickamy/go-trace/tracer"
)

func TestEventType_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		et   tracer.EventType
		want string
	}{
		{"span_start", tracer.EventSpanStart, "span_start"},
		{"span_end", tracer.EventSpanEnd, "span_end"},
		{"unknown", tracer.EventType(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.et.String(); got != tt.want {
				t.Errorf("EventType.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewSpanStartEvent(t *testing.T) {
	t.Parallel()

	now := time.Now()
	ev := tracer.NewSpanStartEvent("s1", "t1", "p1", "Handler", tracer.SpanKindHTTP, now)

	if ev.Type != tracer.EventSpanStart {
		t.Errorf("Type = %v, want %v", ev.Type, tracer.EventSpanStart)
	}
	if ev.SpanID != "s1" {
		t.Errorf("SpanID = %q, want %q", ev.SpanID, "s1")
	}
	if ev.TraceID != "t1" {
		t.Errorf("TraceID = %q, want %q", ev.TraceID, "t1")
	}
	if ev.ParentID != "p1" {
		t.Errorf("ParentID = %q, want %q", ev.ParentID, "p1")
	}
	if ev.Name != "Handler" {
		t.Errorf("Name = %q, want %q", ev.Name, "Handler")
	}
	if ev.Kind != tracer.SpanKindHTTP {
		t.Errorf("Kind = %v, want %v", ev.Kind, tracer.SpanKindHTTP)
	}
	if !ev.Time.Equal(now) {
		t.Errorf("Time = %v, want %v", ev.Time, now)
	}
}

func TestNewSpanStartEvent_NoParent(t *testing.T) {
	t.Parallel()

	ev := tracer.NewSpanStartEvent("s1", "t1", "", "Root", tracer.SpanKindFunction, time.Now())

	if ev.ParentID != "" {
		t.Errorf("ParentID = %q, want empty", ev.ParentID)
	}
}

func TestNewSpanEndEvent(t *testing.T) {
	t.Parallel()

	now := time.Now()
	attrs := map[string]string{"query": "SELECT 1", "rows": "3"}
	ev := tracer.NewSpanEndEvent("s1", "t1", now, attrs)

	if ev.Type != tracer.EventSpanEnd {
		t.Errorf("Type = %v, want %v", ev.Type, tracer.EventSpanEnd)
	}
	if ev.SpanID != "s1" {
		t.Errorf("SpanID = %q, want %q", ev.SpanID, "s1")
	}
	if ev.TraceID != "t1" {
		t.Errorf("TraceID = %q, want %q", ev.TraceID, "t1")
	}
	if !ev.Time.Equal(now) {
		t.Errorf("Time = %v, want %v", ev.Time, now)
	}
	if ev.Attrs["query"] != "SELECT 1" {
		t.Errorf("Attrs[query] = %q, want %q", ev.Attrs["query"], "SELECT 1")
	}
	if ev.Attrs["rows"] != "3" {
		t.Errorf("Attrs[rows] = %q, want %q", ev.Attrs["rows"], "3")
	}
}

func TestNewSpanEndEvent_AttrsNotShared(t *testing.T) {
	t.Parallel()

	attrs := map[string]string{"key": "original"}
	ev := tracer.NewSpanEndEvent("s1", "t1", time.Now(), attrs)

	attrs["key"] = "mutated"

	if ev.Attrs["key"] != "original" {
		t.Errorf("Attrs[key] = %q, want %q (defensive copy failed)", ev.Attrs["key"], "original")
	}
}

func TestNewSpanEndEvent_NilAttrs(t *testing.T) {
	t.Parallel()

	ev := tracer.NewSpanEndEvent("s1", "t1", time.Now(), nil)

	if ev.Attrs != nil {
		t.Errorf("Attrs = %v, want nil", ev.Attrs)
	}
}
