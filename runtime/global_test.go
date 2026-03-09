package runtime_test

import (
	"testing"

	"github.com/mickamy/go-trace/runtime"
	"github.com/mickamy/go-trace/tracer"
)

func TestGlobalTracer_NoopWithoutSocket(t *testing.T) {
	t.Parallel()

	// GOTRACE_SOCKET is not set, so GlobalTracer returns a no-op tracer.
	// Calling Enter should not panic.
	tr := runtime.GlobalTracer()
	if tr == nil {
		t.Fatal("GlobalTracer() returned nil")
	}

	ctx := t.Context()
	_, finish := tr.Enter(ctx, "Test", tracer.SpanKindFunction)
	finish(nil)
}
