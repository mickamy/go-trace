package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"

	"github.com/mickamy/go-trace/config"
	"github.com/mickamy/go-trace/instrument"
	"github.com/mickamy/go-trace/tracer"
)

const version = "dev"

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
