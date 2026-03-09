package tracer_test

import (
	"testing"
	"time"

	"github.com/mickamy/go-trace/tracer"
)

func TestSpanKind_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		kind tracer.SpanKind
		want string
	}{
		{"function", tracer.SpanKindFunction, "function"},
		{"http", tracer.SpanKindHTTP, "http"},
		{"sql", tracer.SpanKindSQL, "sql"},
		{"unknown", tracer.SpanKind(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.kind.String(); got != tt.want {
				t.Errorf("SpanKind.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewSpan(t *testing.T) {
	t.Parallel()

	now := time.Now()
	end := now.Add(10 * time.Millisecond)

	span := tracer.NewSpan("span-1", "trace-1", "Handler", tracer.SpanKindFunction, now, end)

	if span.ID != "span-1" {
		t.Errorf("ID = %q, want %q", span.ID, "span-1")
	}
	if span.TraceID != "trace-1" {
		t.Errorf("TraceID = %q, want %q", span.TraceID, "trace-1")
	}
	if span.Name != "Handler" {
		t.Errorf("Name = %q, want %q", span.Name, "Handler")
	}
	if span.Kind != tracer.SpanKindFunction {
		t.Errorf("Kind = %v, want %v", span.Kind, tracer.SpanKindFunction)
	}
	if span.ParentID != "" {
		t.Errorf("ParentID = %q, want empty", span.ParentID)
	}
	if span.Attrs != nil {
		t.Errorf("Attrs = %v, want nil", span.Attrs)
	}
	if span.Children != nil {
		t.Errorf("Children = %v, want nil", span.Children)
	}
}

func TestSpan_Duration(t *testing.T) {
	t.Parallel()

	now := time.Now()
	span := tracer.NewSpan("s1", "t1", "op", tracer.SpanKindFunction, now, now.Add(15*time.Millisecond))

	if got := span.Duration(); got != 15*time.Millisecond {
		t.Errorf("Duration() = %v, want %v", got, 15*time.Millisecond)
	}
}

func TestSpan_WithParentID(t *testing.T) {
	t.Parallel()

	original := tracer.NewSpan("s1", "t1", "op", tracer.SpanKindFunction, time.Now(), time.Now())
	modified := original.WithParentID("parent-1")

	if modified.ParentID != "parent-1" {
		t.Errorf("modified.ParentID = %q, want %q", modified.ParentID, "parent-1")
	}
	if original.ParentID != "" {
		t.Errorf("original mutated: ParentID = %q, want empty", original.ParentID)
	}
}

func TestSpan_WithAttr(t *testing.T) {
	t.Parallel()

	t.Run("add to empty", func(t *testing.T) {
		t.Parallel()

		original := tracer.NewSpan("s1", "t1", "op", tracer.SpanKindFunction, time.Now(), time.Now())
		modified := original.WithAttr("query", "SELECT 1")

		if modified.Attrs["query"] != "SELECT 1" {
			t.Errorf("Attrs[query] = %q, want %q", modified.Attrs["query"], "SELECT 1")
		}
		if original.Attrs != nil {
			t.Errorf("original mutated: Attrs = %v, want nil", original.Attrs)
		}
	})

	t.Run("add to existing", func(t *testing.T) {
		t.Parallel()

		span := tracer.NewSpan("s1", "t1", "op", tracer.SpanKindSQL, time.Now(), time.Now()).
			WithAttr("query", "SELECT 1")
		modified := span.WithAttr("rows", "5")

		if modified.Attrs["query"] != "SELECT 1" {
			t.Errorf("Attrs[query] = %q, want %q", modified.Attrs["query"], "SELECT 1")
		}
		if modified.Attrs["rows"] != "5" {
			t.Errorf("Attrs[rows] = %q, want %q", modified.Attrs["rows"], "5")
		}
		if len(span.Attrs) != 1 {
			t.Errorf("original mutated: len(Attrs) = %d, want 1", len(span.Attrs))
		}
	})
}

func TestSpan_WithChild(t *testing.T) {
	t.Parallel()

	t.Run("add single child", func(t *testing.T) {
		t.Parallel()

		parent := tracer.NewSpan("p1", "t1", "parent", tracer.SpanKindFunction, time.Now(), time.Now())
		child := tracer.NewSpan("c1", "t1", "child", tracer.SpanKindSQL, time.Now(), time.Now())
		modified := parent.WithChild(child)

		if len(modified.Children) != 1 {
			t.Fatalf("len(Children) = %d, want 1", len(modified.Children))
		}
		if modified.Children[0].ID != "c1" {
			t.Errorf("Children[0].ID = %q, want %q", modified.Children[0].ID, "c1")
		}
		if len(parent.Children) != 0 {
			t.Errorf("original mutated: len(Children) = %d, want 0", len(parent.Children))
		}
	})

	t.Run("add multiple children", func(t *testing.T) {
		t.Parallel()

		now := time.Now()
		span := tracer.NewSpan("p1", "t1", "parent", tracer.SpanKindHTTP, now, now).
			WithChild(tracer.NewSpan("c1", "t1", "first", tracer.SpanKindFunction, now, now)).
			WithChild(tracer.NewSpan("c2", "t1", "second", tracer.SpanKindSQL, now, now))

		if len(span.Children) != 2 {
			t.Fatalf("len(Children) = %d, want 2", len(span.Children))
		}
		if span.Children[0].ID != "c1" {
			t.Errorf("Children[0].ID = %q, want %q", span.Children[0].ID, "c1")
		}
		if span.Children[1].ID != "c2" {
			t.Errorf("Children[1].ID = %q, want %q", span.Children[1].ID, "c2")
		}
	})
}
