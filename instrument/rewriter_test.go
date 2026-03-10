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

func TestRewrite_BlankContextParam(t *testing.T) {
	t.Parallel()

	src := []byte(`package main

import "context"

func Handle(_ context.Context) {
	println("hello")
}
`)

	got, err := instrument.Rewrite(src)
	if err != nil {
		t.Fatalf("Rewrite() error: %v", err)
	}

	result := string(got)
	if !strings.Contains(result, "__gotraceTracer.Enter") {
		t.Error("function with blank context param should be instrumented")
	}
	if !strings.Contains(result, "__gotraceCtx") {
		t.Error("blank identifier should be renamed to __gotraceCtx")
	}
	if strings.Contains(result, "Enter(_,") {
		t.Error("should not use _ as value in Enter call")
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

// --- SQL rewriting tests ---

func TestRewrite_SQLOpen(t *testing.T) {
	t.Parallel()

	src := []byte(`package main

import "database/sql"

func setup() {
	db, err := sql.Open("postgres", "host=localhost")
	_ = db
	_ = err
}
`)

	got, err := instrument.Rewrite(src)
	if err != nil {
		t.Fatalf("Rewrite() error: %v", err)
	}

	result := string(got)
	if !strings.Contains(result, "gotraceruntime.OpenDB(__gotraceTracer,") {
		t.Errorf("expected gotraceruntime.OpenDB call, got:\n%s", result)
	}
	if strings.Contains(result, "sql.Open") {
		t.Error("sql.Open should be replaced")
	}
	if strings.Contains(result, `"database/sql"`) {
		t.Error("unused database/sql import should be removed")
	}
}

func TestRewrite_SQLOpen_PreservesImportWhenStillUsed(t *testing.T) {
	t.Parallel()

	src := []byte(`package main

import "database/sql"

func setup() {
	db, err := sql.Open("postgres", "host=localhost")
	_ = db
	_ = err
	_ = sql.ErrNoRows
}
`)

	got, err := instrument.Rewrite(src)
	if err != nil {
		t.Fatalf("Rewrite() error: %v", err)
	}

	result := string(got)
	if !strings.Contains(result, `"database/sql"`) {
		t.Error("database/sql import should be preserved when sql is still referenced")
	}
}

func TestRewrite_SQLOpen_AlreadyRewritten(t *testing.T) {
	t.Parallel()

	src := []byte(`package main

import gotraceruntime "github.com/mickamy/go-trace/runtime"

func setup() {
	db, err := gotraceruntime.OpenDB(__gotraceTracer, "postgres", "host=localhost")
	_ = db
	_ = err
}
`)

	got, err := instrument.Rewrite(src)
	if err != nil {
		t.Fatalf("Rewrite() error: %v", err)
	}

	count := strings.Count(string(got), "OpenDB")
	if count != 1 {
		t.Errorf("OpenDB count = %d, want 1 (should not double-wrap)", count)
	}
}

func TestRewrite_SQLOpen_MultipleCalls(t *testing.T) {
	t.Parallel()

	src := []byte(`package main

import "database/sql"

func setup() {
	db1, _ := sql.Open("postgres", "dsn1")
	db2, _ := sql.Open("mysql", "dsn2")
	_ = db1
	_ = db2
}
`)

	got, err := instrument.Rewrite(src)
	if err != nil {
		t.Fatalf("Rewrite() error: %v", err)
	}

	count := strings.Count(string(got), "gotraceruntime.OpenDB")
	if count != 2 {
		t.Errorf("OpenDB count = %d, want 2", count)
	}
}

// --- HTTP rewriting tests ---

func TestRewrite_HTTPListenAndServe(t *testing.T) {
	t.Parallel()

	src := []byte(`package main

import "net/http"

func main() {
	http.ListenAndServe(":8080", mux)
}
`)

	got, err := instrument.Rewrite(src)
	if err != nil {
		t.Fatalf("Rewrite() error: %v", err)
	}

	result := string(got)
	if !strings.Contains(result, "gotraceruntime.Middleware(__gotraceTracer, mux)") {
		t.Errorf("expected Middleware wrapping, got:\n%s", result)
	}
}

func TestRewrite_HTTPListenAndServeTLS(t *testing.T) {
	t.Parallel()

	src := []byte(`package main

import "net/http"

func main() {
	http.ListenAndServeTLS(":443", "cert.pem", "key.pem", handler)
}
`)

	got, err := instrument.Rewrite(src)
	if err != nil {
		t.Fatalf("Rewrite() error: %v", err)
	}

	result := string(got)
	if !strings.Contains(result, "gotraceruntime.Middleware(__gotraceTracer, handler)") {
		t.Errorf("expected Middleware wrapping, got:\n%s", result)
	}
}

func TestRewrite_HTTPListenAndServe_AlreadyWrapped(t *testing.T) {
	t.Parallel()

	src := []byte(`package main

import (
	"net/http"
	gotraceruntime "github.com/mickamy/go-trace/runtime"
)

func main() {
	http.ListenAndServe(":8080", gotraceruntime.Middleware(__gotraceTracer, mux))
}
`)

	got, err := instrument.Rewrite(src)
	if err != nil {
		t.Fatalf("Rewrite() error: %v", err)
	}

	count := strings.Count(string(got), "Middleware")
	if count != 1 {
		t.Errorf("Middleware count = %d, want 1 (should not double-wrap)", count)
	}
}
