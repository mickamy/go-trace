//nolint:gosec // test file reads from temp dirs
package instrument_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mickamy/go-trace/config"
	"github.com/mickamy/go-trace/instrument"
)

func setupProject(t *testing.T, files map[string]string) string {
	t.Helper()

	dir := t.TempDir()
	for path, content := range files {
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	return dir
}

func TestInjector_RewritesMatchingFiles(t *testing.T) {
	t.Parallel()

	srcDir := setupProject(t, map[string]string{
		"handler/user.go": `package handler

import "context"

type UserHandler struct{}

func (h *UserHandler) Get(ctx context.Context) {}
`,
		"util/strings.go": `package util

import "context"

func ToUpper(ctx context.Context, s string) string { return s }
`,
	})

	cfg := config.Config{
		Instrument: config.InstrumentConfig{
			Include: []string{"**/handler/**"},
		},
	}

	inj := instrument.NewInjector(cfg)
	tmpDir, err := inj.Inject(srcDir)
	if err != nil {
		t.Fatalf("Inject() error: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })

	handlerData, err := os.ReadFile(filepath.Join(tmpDir, "handler", "user.go"))
	if err != nil {
		t.Fatalf("read handler: %v", err)
	}
	if !strings.Contains(string(handlerData), "__gotraceTracer.Enter") {
		t.Error("handler/user.go should be instrumented")
	}

	utilData, err := os.ReadFile(filepath.Join(tmpDir, "util", "strings.go"))
	if err != nil {
		t.Fatalf("read util: %v", err)
	}
	if strings.Contains(string(utilData), "__gotraceTracer.Enter") {
		t.Error("util/strings.go should NOT be instrumented")
	}
}

func TestInjector_SkipsTestFiles(t *testing.T) {
	t.Parallel()

	srcDir := setupProject(t, map[string]string{
		"handler/user.go": `package handler

import "context"

func Get(ctx context.Context) {}
`,
		"handler/user_test.go": `package handler_test

import "context"

func TestGet(ctx context.Context) {}
`,
	})

	cfg := config.Default()
	inj := instrument.NewInjector(cfg)
	tmpDir, err := inj.Inject(srcDir)
	if err != nil {
		t.Fatalf("Inject() error: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })

	testData, err := os.ReadFile(filepath.Join(tmpDir, "handler", "user_test.go"))
	if err != nil {
		t.Fatalf("read test file: %v", err)
	}
	if strings.Contains(string(testData), "__gotraceTracer.Enter") {
		t.Error("test files should NOT be instrumented")
	}
}

func TestInjector_SkipsGitDir(t *testing.T) {
	t.Parallel()

	srcDir := setupProject(t, map[string]string{
		".git/config": "[core]\n",
		"handler/user.go": `package handler

import "context"

func Get(ctx context.Context) {}
`,
	})

	cfg := config.Default()
	inj := instrument.NewInjector(cfg)
	tmpDir, err := inj.Inject(srcDir)
	if err != nil {
		t.Fatalf("Inject() error: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })

	if _, err := os.Stat(filepath.Join(tmpDir, ".git")); !os.IsNotExist(err) {
		t.Error(".git directory should be skipped")
	}
}

func TestInjector_CopiesNonGoFiles(t *testing.T) {
	t.Parallel()

	srcDir := setupProject(t, map[string]string{
		"go.mod":     "module example.com/app\n\ngo 1.26\n",
		"README.md":  "# App\n",
		"handler.go": "package main\n",
	})

	cfg := config.Default()
	inj := instrument.NewInjector(cfg)
	tmpDir, err := inj.Inject(srcDir)
	if err != nil {
		t.Fatalf("Inject() error: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })

	for _, name := range []string{"go.mod", "README.md", "handler.go"} {
		if _, err := os.Stat(filepath.Join(tmpDir, name)); err != nil {
			t.Errorf("%s should be copied: %v", name, err)
		}
	}
}

func TestInjector_DefaultConfig_InstrumentsAllGoFiles(t *testing.T) {
	t.Parallel()

	srcDir := setupProject(t, map[string]string{
		"a.go": `package main

import "context"

func A(ctx context.Context) {}
`,
		"sub/b.go": `package sub

import "context"

func B(ctx context.Context) {}
`,
	})

	cfg := config.Default()
	inj := instrument.NewInjector(cfg)
	tmpDir, err := inj.Inject(srcDir)
	if err != nil {
		t.Fatalf("Inject() error: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })

	for _, path := range []string{"a.go", "sub/b.go"} {
		data, err := os.ReadFile(filepath.Join(tmpDir, path))
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if !strings.Contains(string(data), "__gotraceTracer.Enter") {
			t.Errorf("%s should be instrumented with default config", path)
		}
	}
}
