package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mickamy/go-trace/analysis"
	"github.com/mickamy/go-trace/config"
	"github.com/mickamy/go-trace/display"
	"github.com/mickamy/go-trace/instrument"
	"github.com/mickamy/go-trace/tracer"
	"github.com/mickamy/go-trace/tui"
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
		err = viewCmd(flag.Args()[1:])
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
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	configPath := fs.String("config", ".go-trace.yaml", "config file path")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: go-trace run [flags] <dir>\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}

	pkg := fs.Arg(0)
	if pkg == "" {
		fs.Usage()
		return errors.New("directory argument is required")
	}

	return run(pkg, *configPath)
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

	if err := col.Listen(ctx); err != nil {
		return fmt.Errorf("collector listen: %w", err)
	}

	collectorErr := make(chan error, 1)
	go func() {
		collectorErr <- col.Serve(ctx)
	}()
	defer func() {
		stop()
		<-collectorErr
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
	return runPlain(ctx, col, binPath, socketPath)
}

func startViewServer(ctx context.Context, col *tracer.Collector) (*tracer.ViewServer, error) {
	sockPath := filepath.Join(os.TempDir(), fmt.Sprintf("go-trace-view-%d.sock", os.Getpid()))
	srv := tracer.NewViewServer(sockPath)
	col.OnSpanComplete(func(_ string, span tracer.Span) {
		srv.Broadcast(span)
	})

	if err := srv.Listen(ctx); err != nil {
		return nil, fmt.Errorf("view server listen: %w", err)
	}

	go func() {
		if err := srv.Serve(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "view server: %v\n", err)
		}
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

	appCmd := exec.CommandContext(ctx, binPath)
	appCmd.Stdout = os.Stdout
	appCmd.Stderr = os.Stderr
	appCmd.Env = append(os.Environ(), "GOTRACE_SOCKET="+socketPath)

	if err := appCmd.Run(); err != nil && ctx.Err() == nil {
		return fmt.Errorf("run: %w", err)
	}
	return nil
}

func viewCmd(args []string) error {
	fs := flag.NewFlagSet("view", flag.ContinueOnError)
	configPath := fs.String("config", ".go-trace.yaml", "config file path")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: go-trace view [flags]\n\nFlags:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}

	sockPath, err := discoverViewSocket()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	mg, err := loadMatchingGroups(*configPath)
	if err != nil {
		return err
	}

	model := tui.New(mg)
	p := tea.NewProgram(model, tea.WithAltScreen())

	bridge := tui.NewBridge(p)

	client := tracer.NewViewClient(sockPath)
	go func() {
		if err := client.Run(ctx, bridge.OnSpan); err != nil {
			p.Send(tui.ErrorMsg{Err: fmt.Errorf("view client: %w", err)})
			return
		}
		p.Send(tui.AppExitMsg{})
	}()

	result, err := p.Run()
	if err != nil {
		return fmt.Errorf("tui: %w", err)
	}
	if m, ok := result.(tui.Model); ok && m.Err() != nil {
		return fmt.Errorf("view: %w", m.Err())
	}
	return nil
}

// discoverViewSocket finds the view server socket for the current session.
// If exactly one session is running, it connects automatically.
// If multiple sessions are found, it returns an error listing them.
func discoverViewSocket() (string, error) {
	pattern := filepath.Join(os.TempDir(), "go-trace-view-*.sock")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", fmt.Errorf("discover sessions: %w", err)
	}

	// Filter to sockets that are actually connectable.
	// Stale sockets from unclean shutdowns are removed.
	// Each socket gets its own timeout so a slow dial doesn't
	// consume the budget for subsequent candidates.
	var d net.Dialer

	var alive []string
	for _, m := range matches {
		dialCtx, dialCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		conn, err := d.DialContext(dialCtx, "unix", m)
		dialCancel()
		if err != nil {
			// Stale socket — clean it up.
			_ = os.Remove(m)
			continue
		}
		_ = conn.Close()
		alive = append(alive, m)
	}

	switch len(alive) {
	case 0:
		return "", errors.New("no running go-trace session found; start one with: go-trace run <dir>")
	case 1:
		return alive[0], nil
	default:
		var b strings.Builder
		b.WriteString("multiple go-trace sessions found:\n")
		for _, s := range alive {
			b.WriteString("  " + s + "\n")
		}
		return "", errors.New(b.String())
	}
}

// loadMatchingGroups reads the config file and returns compiled MatchingGroups.
// Returns nil (no grouping) if the config file doesn't exist or has no patterns.
func loadMatchingGroups(path string) (*analysis.MatchingGroups, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil, nil //nolint:nilnil // no config file means no grouping
	}

	cfg, err := config.Load(path)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	if len(cfg.Analysis.MatchingGroups) == 0 {
		return nil, nil //nolint:nilnil // no patterns means no grouping
	}

	mg, err := analysis.NewMatchingGroups(cfg.Analysis.MatchingGroups)
	if err != nil {
		return nil, fmt.Errorf("compile matching groups: %w", err)
	}
	return mg, nil
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
