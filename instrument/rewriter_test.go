package instrument_test

import (
	"strings"
	"testing"

	"github.com/mickamy/go-trace/instrument"
)

func TestRewrite_ExportedFuncWithContext(t *testing.T) {
	t.Parallel()

	src := []byte(`package main

import "context"

func Handle(ctx context.Context) {
	_ = ctx
}
`)

	got, err := instrument.Rewrite(src)
	if err != nil {
		t.Fatalf("Rewrite() error: %v", err)
	}

	result := string(got)
	if !strings.Contains(result, "__gotraceTracer.Enter") {
		t.Error("expected __gotraceTracer.Enter call")
	}
	if !strings.Contains(result, "defer __gotraceFinish(nil)") {
		t.Error("expected defer __gotraceFinish(nil)")
	}
	if !strings.Contains(result, `gotraceruntime "github.com/mickamy/go-trace/runtime"`) {
		t.Error("expected runtime import")
	}
}

func TestRewrite_MethodWithReceiver(t *testing.T) {
	t.Parallel()

	src := []byte(`package main

import "context"

type Svc struct{}

func (s *Svc) Run(ctx context.Context) {
	_ = ctx
}
`)

	got, err := instrument.Rewrite(src)
	if err != nil {
		t.Fatalf("Rewrite() error: %v", err)
	}

	result := string(got)
	if !strings.Contains(result, `"Svc.Run"`) {
		t.Errorf("expected span name Svc.Run, got:\n%s", result)
	}
}

func TestRewrite_UnexportedFuncSkipped(t *testing.T) {
	t.Parallel()

	src := []byte(`package main

import "context"

func handle(ctx context.Context) {
	_ = ctx
}
`)

	got, err := instrument.Rewrite(src)
	if err != nil {
		t.Fatalf("Rewrite() error: %v", err)
	}

	if strings.Contains(string(got), "__gotraceTracer") {
		t.Error("unexported function should not be instrumented")
	}
}

func TestRewrite_NoContextParamSkipped(t *testing.T) {
	t.Parallel()

	src := []byte(`package main

func Handle(name string) {
	_ = name
}
`)

	got, err := instrument.Rewrite(src)
	if err != nil {
		t.Fatalf("Rewrite() error: %v", err)
	}

	if strings.Contains(string(got), "__gotraceTracer") {
		t.Error("function without context.Context should not be instrumented")
	}
}

func TestRewrite_AlreadyInstrumented(t *testing.T) {
	t.Parallel()

	src := []byte(`package main

import "context"

func Handle(ctx context.Context) {
	ctx, __gotraceFinish := __gotraceTracer.Enter(ctx, "Handle", 0)
	defer __gotraceFinish(nil)
	_ = ctx
}
`)

	got, err := instrument.Rewrite(src)
	if err != nil {
		t.Fatalf("Rewrite() error: %v", err)
	}

	count := strings.Count(string(got), "__gotraceTracer.Enter")
	if count != 1 {
		t.Errorf("Enter call count = %d, want 1 (should not duplicate)", count)
	}
}

func TestRewrite_MultipleFuncs(t *testing.T) {
	t.Parallel()

	src := []byte(`package main

import "context"

func First(ctx context.Context) {
	_ = ctx
}

func Second(ctx context.Context) {
	_ = ctx
}

func private(ctx context.Context) {
	_ = ctx
}
`)

	got, err := instrument.Rewrite(src)
	if err != nil {
		t.Fatalf("Rewrite() error: %v", err)
	}

	result := string(got)
	count := strings.Count(result, "__gotraceTracer.Enter")
	if count != 2 {
		t.Errorf("Enter call count = %d, want 2", count)
	}
}

func TestRewrite_NoModification_ReturnsSameSource(t *testing.T) {
	t.Parallel()

	src := []byte(`package main

func main() {
	println("hello")
}
`)

	got, err := instrument.Rewrite(src)
	if err != nil {
		t.Fatalf("Rewrite() error: %v", err)
	}

	if string(got) != string(src) {
		t.Error("source without targets should be returned unchanged")
	}
}

func TestRewrite_InvalidSource(t *testing.T) {
	t.Parallel()

	src := []byte(`this is not valid go`)

	_, err := instrument.Rewrite(src)
	if err == nil {
		t.Error("expected error for invalid source")
	}
}
