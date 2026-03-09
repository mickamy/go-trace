package instrument

import (
	"bytes"
	"fmt"
	"go/parser"
	"go/token"
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

	instrumentedPkgs, err := inj.copyAndRewrite(srcDir, tmpDir)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", fmt.Errorf("inject: %w", err)
	}

	if err := writeInitFiles(tmpDir, instrumentedPkgs); err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", fmt.Errorf("write init files: %w", err)
	}

	return tmpDir, nil
}

// copyAndRewrite copies the project and rewrites matching files.
// Returns a map of directory path → package name for instrumented packages.
func (inj Injector) copyAndRewrite(srcDir, dstDir string) (map[string]string, error) {
	instrumentedPkgs := make(map[string]string)

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

		rewritten, err := inj.processFile(relPath, path, dstPath)
		if err != nil {
			return err
		}
		if rewritten {
			dir := filepath.Dir(relPath)
			if _, exists := instrumentedPkgs[dir]; !exists {
				pkgName := detectPackageName(path)
				if pkgName != "" {
					instrumentedPkgs[dir] = pkgName
				}
			}
		}

		return nil
	}); err != nil {
		return nil, fmt.Errorf("walk source dir: %w", err)
	}

	return instrumentedPkgs, nil
}

// processFile copies a file, rewriting it if it matches.
// Returns true if the file was rewritten.
func (inj Injector) processFile(relPath, srcPath, dstPath string) (bool, error) {
	data, err := os.ReadFile(srcPath) //nolint:gosec // walking user's project
	if err != nil {
		return false, fmt.Errorf("read %s: %w", relPath, err)
	}

	rewritten := false
	if isGoSource(relPath) && inj.cfg.Match(relPath) {
		out, err := Rewrite(data)
		if err != nil {
			return false, fmt.Errorf("rewrite %s: %w", relPath, err)
		}
		if !bytes.Equal(out, data) {
			data = out
			rewritten = true
		}
	}

	if err := os.WriteFile(dstPath, data, 0o600); err != nil { //nolint:gosec // dstPath is within our temp dir
		return false, fmt.Errorf("write %s: %w", relPath, err)
	}
	return rewritten, nil
}

// writeInitFiles generates gotrace_init.go for each instrumented package.
func writeInitFiles(dstDir string, pkgs map[string]string) error {
	for dir, pkgName := range pkgs {
		initPath := filepath.Join(dstDir, dir, "gotrace_init.go")
		content := fmt.Sprintf(`package %s

import gotraceruntime %q

var __gotraceTracer = gotraceruntime.GlobalTracer()
`, pkgName, "github.com/mickamy/go-trace/runtime")

		if err := os.WriteFile(initPath, []byte(content), 0o600); err != nil {
			return fmt.Errorf("write %s: %w", initPath, err)
		}
	}
	return nil
}

// detectPackageName parses a Go file to extract its package name.
func detectPackageName(path string) string {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.PackageClauseOnly)
	if err != nil {
		return ""
	}
	return f.Name.Name
}

func shouldSkipDir(d fs.DirEntry) bool {
	return d.IsDir() && d.Name() == ".git"
}

func isGoSource(path string) bool {
	return strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go")
}
