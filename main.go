package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/mickamy/go-trace/config"
	"github.com/mickamy/go-trace/instrument"
	"github.com/mickamy/go-trace/tracer"
)

const version = "dev"

const goTraceModule = "github.com/mickamy/go-trace"

func main() {
	showVersion := flag.Bool("version", false, "print version")
	configPath := flag.String("config", ".go-trace.yaml", "config file path")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: go-trace [flags] <package>\n\n")
		fmt.Fprintf(os.Stderr, "Trace HTTP, SQL, and function calls in your Go app.\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *showVersion {
		fmt.Println("go-trace", version)
		return
	}

	pkg := flag.Arg(0)
	if pkg == "" {
		flag.Usage()
		os.Exit(1)
	}

	if err := run(pkg, *configPath); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(pkg, configPath string) error {
	cfg := config.Default()
	if _, err := os.Stat(configPath); err == nil {
		loaded, err := config.Load(configPath)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		cfg = loaded
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// 1. Instrument source
	inj := instrument.NewInjector(cfg)
	tmpDir, err := inj.Inject(pkg)
	if err != nil {
		return fmt.Errorf("instrument: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// 1.5. Add go-trace runtime dependency to instrumented project
	if err := addRuntimeDep(ctx, tmpDir); err != nil {
		return fmt.Errorf("add runtime dep: %w", err)
	}

	// 2. Start collector
	socketPath := filepath.Join(tmpDir, "go-trace.sock")
	col := tracer.NewCollector(socketPath)

	col.OnSpanComplete(func(_ string, span tracer.Span) {
		// Temporary stdout output until TUI is implemented.
		fmt.Printf("[%s] %s %s (%v)\n", span.Kind, span.Name, span.ID, span.Duration())
	})

	collectorErr := make(chan error, 1)
	go func() {
		collectorErr <- col.Start(ctx)
	}()

	// 3. Build instrumented binary
	binPath := filepath.Join(tmpDir, "app")
	buildCmd := exec.CommandContext(ctx, "go", "build", "-o", binPath, ".") //nolint:gosec // building user's project
	buildCmd.Dir = tmpDir
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	buildCmd.Env = append(os.Environ(), "GOTRACE_SOCKET="+socketPath)

	if err := buildCmd.Run(); err != nil {
		return fmt.Errorf("build: %w", err)
	}

	// 4. Run instrumented binary
	appCmd := exec.CommandContext(ctx, binPath) //nolint:gosec // running instrumented binary
	appCmd.Stdout = os.Stdout
	appCmd.Stderr = os.Stderr
	appCmd.Env = append(os.Environ(), "GOTRACE_SOCKET="+socketPath)

	if err := appCmd.Run(); err != nil && ctx.Err() == nil {
		return fmt.Errorf("run: %w", err)
	}

	stop()
	<-collectorErr

	return nil
}

// addRuntimeDep adds github.com/mickamy/go-trace as a dependency
// to the instrumented project so it can import the runtime package.
func addRuntimeDep(ctx context.Context, dir string) error {
	editArgs := []string{"mod", "edit"}

	if version == "dev" {
		root, err := findModuleRoot()
		if err != nil {
			return err
		}
		editArgs = append(editArgs,
			"-require="+goTraceModule+"@v0.0.0",
			"-replace="+goTraceModule+"="+root,
		)
	} else {
		v := version
		if !strings.HasPrefix(v, "v") {
			v = "v" + v
		}
		editArgs = append(editArgs,
			"-require="+goTraceModule+"@"+v,
		)
	}

	editCmd := exec.CommandContext(ctx, "go", editArgs...)
	editCmd.Dir = dir
	if out, err := editCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go mod edit: %w\n%s", err, out)
	}

	tidyCmd := exec.CommandContext(ctx, "go", "mod", "tidy")
	tidyCmd.Dir = dir
	tidyCmd.Stdout = os.Stdout
	tidyCmd.Stderr = os.Stderr
	if err := tidyCmd.Run(); err != nil {
		return fmt.Errorf("go mod tidy: %w", err)
	}

	return nil
}

// findModuleRoot walks up from the working directory
// to find the go-trace module root.
func findModuleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getwd: %w", err)
	}

	for {
		data, err := os.ReadFile(filepath.Join(dir, "go.mod")) //nolint:gosec // walking known directories
		if err == nil && strings.Contains(string(data), "module "+goTraceModule) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "", errors.New(
		"go-trace module root not found; run from within the go-trace repo in dev mode",
	)
}
