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

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mickamy/go-trace/config"
	"github.com/mickamy/go-trace/display"
	"github.com/mickamy/go-trace/instrument"
	"github.com/mickamy/go-trace/tracer"
	gotui "github.com/mickamy/go-trace/tui"
)

const version = "dev"

const goTraceModule = "github.com/mickamy/go-trace"

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: go-trace <command> [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Commands:\n")
		fmt.Fprintf(os.Stderr, "  run      Instrument, build, and run a Go package\n")
		fmt.Fprintf(os.Stderr, "  view     Connect to a running session and display traces in TUI\n")
		fmt.Fprintf(os.Stderr, "  version  Print version\n")
	}
	flag.Parse()

	cmd := flag.Arg(0)
	var err error
	switch cmd {
	case "run":
		err = runCmd(flag.Args()[1:])
	case "view":
		err = viewCmd()
	case "version":
		fmt.Println("go-trace", version)
	default:
		flag.Usage()
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func runCmd(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	configPath := fs.String("config", ".go-trace.yaml", "config file path")
	useTUI := fs.Bool("tui", false, "launch TUI instead of stderr output")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: go-trace run [flags] <package>\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	pkg := fs.Arg(0)
	if pkg == "" {
		fs.Usage()
		os.Exit(1)
	}

	return run(pkg, *configPath, *useTUI)
}

func run(pkg, configPath string, useTUI bool) error {
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
	if useTUI {
		err = runWithTUI(ctx, col, binPath, socketPath)
	} else {
		err = runPlain(ctx, col, binPath, socketPath)
	}
	if err != nil {
		return err
	}

	stop()
	<-collectorErr

	return nil
}

func startViewServer(ctx context.Context, col *tracer.Collector) (*tracer.ViewServer, error) {
	srv := tracer.NewViewServer(viewSocketPath())
	col.OnSpanComplete(func(_ string, span tracer.Span) {
		srv.Broadcast(span)
	})

	go func() {
		_ = srv.Start(ctx)
	}()

	return srv, nil
}

func runPlain(ctx context.Context, col *tracer.Collector, binPath, socketPath string) error {
	srv, err := startViewServer(ctx, col)
	if err != nil {
		return err
	}
	defer func() { _ = srv.Close() }()

	renderer := display.NewRenderer(os.Stderr)
	col.OnSpanComplete(func(traceID string, span tracer.Span) {
		renderer.Add(traceID, span)
	})

	appCmd := exec.CommandContext(ctx, binPath) //nolint:gosec // running instrumented binary
	appCmd.Stdout = os.Stdout
	appCmd.Stderr = os.Stderr
	appCmd.Env = append(os.Environ(), "GOTRACE_SOCKET="+socketPath)

	if err := appCmd.Run(); err != nil && ctx.Err() == nil {
		return fmt.Errorf("run: %w", err)
	}
	return nil
}

func runWithTUI(ctx context.Context, col *tracer.Collector, binPath, socketPath string) error {
	srv, err := startViewServer(ctx, col)
	if err != nil {
		return err
	}
	defer func() { _ = srv.Close() }()

	model := gotui.New()
	p := tea.NewProgram(model, tea.WithAltScreen())

	bridge := gotui.NewBridge(p)
	col.OnSpanComplete(bridge.OnSpan)

	appCmd := exec.CommandContext(ctx, binPath) //nolint:gosec // running instrumented binary
	appWriter := gotui.NewAppWriter(p)
	appCmd.Stdout = appWriter
	appCmd.Stderr = appWriter
	appCmd.Env = append(os.Environ(), "GOTRACE_SOCKET="+socketPath)

	go func() {
		_ = appCmd.Run()
		p.Send(gotui.AppExitMsg{})
	}()

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("tui: %w", err)
	}
	return nil
}

func viewCmd() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	model := gotui.NewTracesOnly()
	p := tea.NewProgram(model, tea.WithAltScreen())

	bridge := gotui.NewBridge(p)

	client := tracer.NewViewClient(viewSocketPath())
	go func() {
		err := client.Run(ctx, bridge.OnSpan)
		if err != nil {
			p.Send(gotui.AppOutputMsg{
				Line: "connection error: " + err.Error(),
			})
		}
		p.Send(gotui.AppExitMsg{})
	}()

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("tui: %w", err)
	}
	return nil
}

func viewSocketPath() string {
	return filepath.Join(os.TempDir(), "go-trace-view.sock")
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
