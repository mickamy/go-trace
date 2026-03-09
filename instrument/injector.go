package instrument

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/mickamy/go-trace/config"
)

// Injector walks a Go project, rewrites matching files with tracing
// instrumentation, and writes the result to a temporary directory.
type Injector struct {
	cfg config.Config
}

// NewInjector creates an injector with the given config.
func NewInjector(cfg config.Config) Injector {
	return Injector{cfg: cfg}
}

// Inject copies the project at srcDir into a temporary directory,
// rewrites matching Go files with tracing calls, and returns
// the path to the instrumented project.
func (inj Injector) Inject(srcDir string) (string, error) {
	srcDir, err := filepath.Abs(srcDir)
	if err != nil {
		return "", fmt.Errorf("resolve source dir: %w", err)
	}

	tmpDir, err := os.MkdirTemp("", "go-trace-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}

	if err := inj.copyAndRewrite(srcDir, tmpDir); err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", fmt.Errorf("inject: %w", err)
	}

	return tmpDir, nil
}

func (inj Injector) copyAndRewrite(srcDir, dstDir string) error {
	if err := filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return fmt.Errorf("rel path: %w", err)
		}

		if shouldSkipDir(d) {
			return filepath.SkipDir
		}

		dstPath := filepath.Join(dstDir, relPath)

		if d.IsDir() {
			return os.MkdirAll(dstPath, 0o750)
		}

		return inj.processFile(relPath, path, dstPath)
	}); err != nil {
		return fmt.Errorf("walk source dir: %w", err)
	}
	return nil
}

func (inj Injector) processFile(relPath, srcPath, dstPath string) error {
	data, err := os.ReadFile(srcPath) //nolint:gosec // walking user's project
	if err != nil {
		return fmt.Errorf("read %s: %w", relPath, err)
	}

	if isGoSource(relPath) && inj.cfg.Match(relPath) {
		rewritten, err := Rewrite(data)
		if err != nil {
			return fmt.Errorf("rewrite %s: %w", relPath, err)
		}
		data = rewritten
	}

	if err := os.WriteFile(dstPath, data, 0o600); err != nil { //nolint:gosec // dstPath is within our temp dir
		return fmt.Errorf("write %s: %w", relPath, err)
	}
	return nil
}

func shouldSkipDir(d fs.DirEntry) bool {
	return d.IsDir() && d.Name() == ".git"
}

func isGoSource(path string) bool {
	return strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go")
}
