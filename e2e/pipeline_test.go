//nolint:gosec // test executes user-controlled commands in temp dirs
package e2e_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/mickamy/go-trace/config"
	"github.com/mickamy/go-trace/instrument"
	"github.com/mickamy/go-trace/tracer"
)

// repoRoot returns the absolute path of the go-trace repository.
func repoRoot(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine caller file")
	}
	// e2e/pipeline_test.go → parent is repo root
	return filepath.Dir(filepath.Dir(file))
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func runCmd(t *testing.T, ctx context.Context, dir, name string, args ...string) {
	t.Helper()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
}

func TestPipeline_InstrumentBuildRun(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)

	// 1. Create a test Go application.
	srcDir := t.TempDir()
	goMod := fmt.Sprintf("module testapp\n\ngo 1.26\n\n"+
		"require github.com/mickamy/go-trace v0.0.0\n\n"+
		"replace github.com/mickamy/go-trace => %s\n", root)
	writeFile(t, filepath.Join(srcDir, "go.mod"), goMod)
	writeFile(t, filepath.Join(srcDir, "main.go"), `package main

import "context"

func main() {
	ctx := context.Background()
	msg := Greet(ctx, "world")
	_ = msg
}

// Greet is an exported function with context.Context — it will be instrumented.
func Greet(ctx context.Context, name string) string {
	return "hello " + name
}
`)

	// 2. Instrument the source.
	cfg := config.Default()
	inj := instrument.NewInjector(cfg)
	tmpDir, err := inj.Inject(srcDir)
	if err != nil {
		t.Fatalf("Inject: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })

	// Verify instrumentation was applied.
	instrumented, err := os.ReadFile(filepath.Join(tmpDir, "main.go"))
	if err != nil {
		t.Fatalf("read instrumented main.go: %v", err)
	}
	if !strings.Contains(string(instrumented), "__gotraceTracer.Enter") {
		t.Fatal("main.go should contain __gotraceTracer.Enter after instrumentation")
	}

	// Verify gotrace_init.go was generated.
	initFile, err := os.ReadFile(filepath.Join(tmpDir, "gotrace_init.go"))
	if err != nil {
		t.Fatalf("read gotrace_init.go: %v", err)
	}
	if !strings.Contains(string(initFile), "GlobalTracer()") {
		t.Fatal("gotrace_init.go should call GlobalTracer()")
	}

	// 3. Start the collector.
	sockFile, err := os.CreateTemp("", "gt-e2e-*.sock") //nolint:usetesting // t.TempDir() path exceeds Unix socket limit
	if err != nil {
		t.Fatalf("create socket temp file: %v", err)
	}
	socketPath := sockFile.Name()
	_ = sockFile.Close()
	_ = os.Remove(socketPath)
	t.Cleanup(func() { _ = os.Remove(socketPath) })

	col := tracer.NewCollector(socketPath)

	spanCh := make(chan tracer.Span, 10)
	col.OnSpanComplete(func(_ string, span tracer.Span) {
		spanCh <- span
	})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go func() {
		_ = col.Start(ctx)
	}()

	waitForSocket(t, socketPath)

	// 4. Build the instrumented app.
	runCmd(t, ctx, tmpDir, "go", "mod", "tidy")

	binPath := filepath.Join(tmpDir, "testapp")
	runCmd(t, ctx, tmpDir, "go", "build", "-o", binPath, ".")

	// 5. Run the instrumented app with GOTRACE_SOCKET.
	app := exec.CommandContext(ctx, binPath)
	app.Dir = tmpDir
	app.Env = append(os.Environ(), "GOTRACE_SOCKET="+socketPath)
	out, err := app.CombinedOutput()
	if err != nil {
		t.Fatalf("run app: %v\n%s", err, out)
	}

	// 6. Verify the collector received the Greet span.
	select {
	case span := <-spanCh:
		if span.Name != "Greet" {
			t.Errorf("span name = %q, want %q", span.Name, "Greet")
		}
		if span.Kind != tracer.SpanKindFunction {
			t.Errorf("span kind = %v, want %v", span.Kind, tracer.SpanKindFunction)
		}
		if span.TraceID == "" {
			t.Error("span should have a trace ID")
		}
		if span.Duration() <= 0 {
			t.Error("span should have positive duration")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for span from collector")
	}
}

func waitForSocket(t *testing.T, path string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("socket %s not created in time", path)
}
