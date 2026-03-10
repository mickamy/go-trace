package runtime_test

import (
	"context"
	"sync"
	"testing"

	"github.com/mickamy/go-trace/runtime"
	"github.com/mickamy/go-trace/tracer"
)

type recordingSender struct {
	mu     sync.Mutex
	events []tracer.Event
}

func (r *recordingSender) Send(ev tracer.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.events = append(r.events, ev)
}

func (r *recordingSender) Close() error { return nil }

func (r *recordingSender) Events() []tracer.Event {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]tracer.Event, len(r.events))
	copy(out, r.events)
	return out
}

func TestTracer_Enter_SendsStartAndEnd(t *testing.T) {
	t.Parallel()

	rec := &recordingSender{}
	tr := runtime.NewTracer(rec)

	ctx := t.Context()
	_, finish := tr.Enter(ctx, "Handler", tracer.SpanKindFunction)
	finish(nil)

	events := rec.Events()
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2", len(events))
	}

	if events[0].Type != tracer.EventSpanStart {
		t.Errorf("events[0].Type = %v, want SpanStart", events[0].Type)
	}
	if events[0].Name != "Handler" {
		t.Errorf("events[0].Name = %q, want %q", events[0].Name, "Handler")
	}
	if events[0].Kind != tracer.SpanKindFunction {
		t.Errorf("events[0].Kind = %v, want %v", events[0].Kind, tracer.SpanKindFunction)
	}

	if events[1].Type != tracer.EventSpanEnd {
		t.Errorf("events[1].Type = %v, want SpanEnd", events[1].Type)
	}
	if events[1].SpanID != events[0].SpanID {
		t.Errorf("end SpanID = %q, want %q (same as start)", events[1].SpanID, events[0].SpanID)
	}
}

func TestTracer_Enter_GeneratesTraceID(t *testing.T) {
	t.Parallel()

	rec := &recordingSender{}
	tr := runtime.NewTracer(rec)

	_, finish := tr.Enter(t.Context(), "Op", tracer.SpanKindFunction)
	finish(nil)

	events := rec.Events()
	if events[0].TraceID == "" {
		t.Error("TraceID is empty, want auto-generated")
	}
	if events[1].TraceID != events[0].TraceID {
		t.Errorf("end TraceID = %q, want %q (same as start)", events[1].TraceID, events[0].TraceID)
	}
}

func TestTracer_Enter_PropagatesTraceID(t *testing.T) {
	t.Parallel()

	rec := &recordingSender{}
	tr := runtime.NewTracer(rec)

	ctx := t.Context()
	childCtx, finishParent := tr.Enter(ctx, "Parent", tracer.SpanKindHTTP)
	_, finishChild := tr.Enter(childCtx, "Child", tracer.SpanKindFunction)
	finishChild(nil)
	finishParent(nil)

	events := rec.Events()
	if len(events) != 4 {
		t.Fatalf("len(events) = %d, want 4", len(events))
	}

	parentStart := events[0]
	childStart := events[1]

	if childStart.TraceID != parentStart.TraceID {
		t.Errorf("child TraceID = %q, want %q (inherited from parent)", childStart.TraceID, parentStart.TraceID)
	}
	if childStart.ParentID != parentStart.SpanID {
		t.Errorf("child ParentID = %q, want %q (parent's SpanID)", childStart.ParentID, parentStart.SpanID)
	}
}

func TestTracer_Enter_RootHasNoParentID(t *testing.T) {
	t.Parallel()

	rec := &recordingSender{}
	tr := runtime.NewTracer(rec)

	_, finish := tr.Enter(t.Context(), "Root", tracer.SpanKindHTTP)
	finish(nil)

	events := rec.Events()
	if events[0].ParentID != "" {
		t.Errorf("root ParentID = %q, want empty", events[0].ParentID)
	}
}

func TestTracer_Enter_EndAttrs(t *testing.T) {
	t.Parallel()

	rec := &recordingSender{}
	tr := runtime.NewTracer(rec)

	_, finish := tr.Enter(t.Context(), "Query", tracer.SpanKindSQL)
	finish(map[string]string{"query": "SELECT 1", "rows": "5"})

	events := rec.Events()
	endEv := events[1]
	if endEv.Attrs["query"] != "SELECT 1" {
		t.Errorf("Attrs[query] = %q, want %q", endEv.Attrs["query"], "SELECT 1")
	}
	if endEv.Attrs["rows"] != "5" {
		t.Errorf("Attrs[rows] = %q, want %q", endEv.Attrs["rows"], "5")
	}
}

func TestTracer_Stop_PreventsSubsequentSends(t *testing.T) {
	t.Parallel()

	rec := &recordingSender{}
	tr := runtime.NewTracer(rec)

	_, finish1 := tr.Enter(t.Context(), "Before", tracer.SpanKindFunction)
	finish1(nil)

	tr.Stop()

	_, finish2 := tr.Enter(t.Context(), "After", tracer.SpanKindFunction)
	finish2(nil)

	events := rec.Events()
	if len(events) != 2 {
		t.Errorf("len(events) = %d, want 2 (events after Stop should be dropped)", len(events))
	}
}

func TestTracer_Enter_UniqueSpanIDs(t *testing.T) {
	t.Parallel()

	rec := &recordingSender{}
	tr := runtime.NewTracer(rec)

	ctx := t.Context()
	_, f1 := tr.Enter(ctx, "A", tracer.SpanKindFunction)
	_, f2 := tr.Enter(ctx, "B", tracer.SpanKindFunction)
	f1(nil)
	f2(nil)

	events := rec.Events()
	if events[0].SpanID == events[1].SpanID {
		t.Errorf("two spans have same ID %q, want unique", events[0].SpanID)
	}
}

func TestTracer_Enter_CancelledContext(t *testing.T) {
	t.Parallel()

	rec := &recordingSender{}
	tr := runtime.NewTracer(rec)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, finish := tr.Enter(ctx, "Op", tracer.SpanKindFunction)
	finish(nil)

	events := rec.Events()
	if len(events) != 2 {
		t.Errorf("len(events) = %d, want 2 (cancelled context should still record)", len(events))
	}
}
