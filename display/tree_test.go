package display_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/mickamy/go-trace/display"
	"github.com/mickamy/go-trace/tracer"
)

func TestRenderer_SingleSpan(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	r := display.NewRenderer(&buf)

	now := time.Now()
	r.Add("t1", tracer.NewSpan("s1", "t1", "GET /hello", tracer.SpanKindHTTP, now, now.Add(100*time.Microsecond)))

	got := buf.String()
	if !strings.Contains(got, "GET /hello [http] 100µs") {
		t.Errorf("unexpected output:\n%s", got)
	}
}

func TestRenderer_ParentChild(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	r := display.NewRenderer(&buf)

	now := time.Now()
	// Child arrives before root (defer ordering).
	child := tracer.NewSpan("s2", "t1", "Greet", tracer.SpanKindFunction,
		now.Add(10*time.Microsecond), now.Add(14*time.Microsecond)).
		WithParentID("s1")
	root := tracer.NewSpan("s1", "t1", "GET /hello", tracer.SpanKindHTTP,
		now, now.Add(100*time.Microsecond))

	r.Add("t1", child)
	// Tree should NOT be printed yet (root hasn't arrived).
	if buf.Len() > 0 {
		t.Fatal("should not print before root span arrives")
	}

	r.Add("t1", root)

	got := buf.String()
	if !strings.Contains(got, "GET /hello [http]") {
		t.Errorf("missing root span:\n%s", got)
	}
	if !strings.Contains(got, "└── Greet [function]") {
		t.Errorf("missing child with connector:\n%s", got)
	}
}

func TestRenderer_DeepNesting(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	r := display.NewRenderer(&buf)

	now := time.Now()
	sql := tracer.NewSpan("s3", "t1", "SQL Query", tracer.SpanKindSQL,
		now.Add(20*time.Microsecond), now.Add(200*time.Microsecond)).
		WithParentID("s2")
	fn := tracer.NewSpan("s2", "t1", "ListUsers", tracer.SpanKindFunction,
		now.Add(10*time.Microsecond), now.Add(250*time.Microsecond)).
		WithParentID("s1")
	root := tracer.NewSpan("s1", "t1", "GET /users", tracer.SpanKindHTTP,
		now, now.Add(300*time.Microsecond))

	r.Add("t1", sql)
	r.Add("t1", fn)
	r.Add("t1", root)

	got := buf.String()
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d:\n%s", len(lines), got)
	}
	if !strings.Contains(lines[0], "GET /users") {
		t.Errorf("line 0: %s", lines[0])
	}
	if !strings.Contains(lines[1], "└── ListUsers") {
		t.Errorf("line 1: %s", lines[1])
	}
	if !strings.Contains(lines[2], "    └── SQL Query") {
		t.Errorf("line 2: %s", lines[2])
	}
}

func TestRenderer_MultipleChildren(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	r := display.NewRenderer(&buf)

	now := time.Now()
	child1 := tracer.NewSpan("s2", "t1", "First", tracer.SpanKindFunction,
		now.Add(10*time.Microsecond), now.Add(20*time.Microsecond)).
		WithParentID("s1")
	child2 := tracer.NewSpan("s3", "t1", "Second", tracer.SpanKindFunction,
		now.Add(30*time.Microsecond), now.Add(40*time.Microsecond)).
		WithParentID("s1")
	root := tracer.NewSpan("s1", "t1", "GET /multi", tracer.SpanKindHTTP,
		now, now.Add(100*time.Microsecond))

	r.Add("t1", child1)
	r.Add("t1", child2)
	r.Add("t1", root)

	got := buf.String()
	if !strings.Contains(got, "├── First") {
		t.Errorf("first child should use ├──:\n%s", got)
	}
	if !strings.Contains(got, "└── Second") {
		t.Errorf("last child should use └──:\n%s", got)
	}
}

func TestRenderer_IndependentTraces(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	r := display.NewRenderer(&buf)

	now := time.Now()
	r.Add("t1", tracer.NewSpan("s1", "t1", "TraceA", tracer.SpanKindHTTP,
		now, now.Add(100*time.Microsecond)))
	r.Add("t2", tracer.NewSpan("s2", "t2", "TraceB", tracer.SpanKindHTTP,
		now, now.Add(200*time.Microsecond)))

	got := buf.String()
	if !strings.Contains(got, "TraceA") {
		t.Error("should contain TraceA")
	}
	if !strings.Contains(got, "TraceB") {
		t.Error("should contain TraceB")
	}
}
