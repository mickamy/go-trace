# go-trace

Instantly visualize request flows in your Go application — no infrastructure, no code changes.

go-trace rewrites your source code at the AST level to inject tracing, builds the instrumented binary, and displays the
collected traces in real time.

![demo](./demo.gif)

## Features

- **Zero-config tracing** — instruments exported functions, HTTP handlers, and SQL queries automatically
- **AST-based rewriting** — no manual annotation or SDK integration required
- **Real-time TUI** — browse trace trees with collapsible spans, color-coded by kind
- **Multi-session friendly** — run multiple traced apps; `go-trace view` auto-connects when a single session is active
- **No infrastructure** — uses Unix domain sockets for IPC, no external services needed

## Install

### Homebrew

```sh
brew install mickamy/tap/go-trace
```

### From source

```sh
go install github.com/mickamy/go-trace@latest
```

### Build locally

```sh
git clone https://github.com/mickamy/go-trace.git
cd go-trace
make install
```

## Quick start

```sh
# From your module root (directory containing go.mod)
go-trace run .

# In another terminal, open the TUI viewer
go-trace view
```

## What gets instrumented

### Functions

Exported functions and methods with `context.Context` as the first parameter are automatically traced:

```go
// Before (your code)
func HandleRequest(ctx context.Context, id string) error { ... }

// After (instrumented)
func HandleRequest(ctx context.Context, id string) error {
    var __gotraceFinish gotraceruntime.FinishFunc
    ctx, __gotraceFinish = __gotraceTracer.Enter(ctx, "HandleRequest", gotraceruntime.SpanKindFunction)
    defer __gotraceFinish(nil)
    ...
}
```

### HTTP

`http.ListenAndServe` and `http.ListenAndServeTLS` calls are wrapped with tracing middleware that records method, path,
and status code:

```go
// Before
http.ListenAndServe(":8080", mux)

// After
http.ListenAndServe(":8080", gotraceruntime.Middleware(__gotraceTracer, mux))
```

### SQL

`sql.Open` calls are replaced to use a tracing driver that records queries and errors:

```go
// Before
db, err := sql.Open("postgres", dsn)

// After
db, err := gotraceruntime.OpenDB(__gotraceTracer, "postgres", dsn)
```

## Configuration

Place a `.go-trace.yaml` in the directory where you run `go-trace` (or specify `-config path/to/config.yaml`).
The config path is resolved relative to the current working directory:

```yaml
instrument:
  include:
    - "cmd/**/*.go"
    - "internal/handler/**/*.go"
```

The default configuration instruments all Go files (`**/*.go`), excluding test files.

## TUI keybindings

| Key            | Action                       |
|----------------|------------------------------|
| `j` / `Down`   | Next trace                   |
| `k` / `Up`     | Previous trace               |
| `Space`        | Collapse / expand trace      |
| `Ctrl+D`       | Page down                    |
| `Ctrl+U`       | Page up                      |
| `G`            | Jump to bottom (follow mode) |
| `g`            | Jump to top                  |
| `q` / `Ctrl+C` | Quit                         |

## Architecture

```
Your App (instrumented)
    │  Unix socket (JSON lines)
    ▼
Collector ─── assembles spans into trees
    │
    ├──▶ Stderr renderer (go-trace run output)
    │
    └──▶ View Server
            │  Unix socket (JSON lines)
            ▼
        View Client (go-trace view)
            │
            ▼
        Bubbletea TUI
```

## CLI reference

```
go-trace run [flags] <dir>    Instrument, build, and run a Go package
go-trace view                 Connect to a running session and display traces in TUI
go-trace version              Print version
```

### `run` flags

| Flag      | Default          | Description         |
|-----------|------------------|---------------------|
| `-config` | `.go-trace.yaml` | Path to config file |

## Development

```sh
make build     # Build to bin/go-trace
make test      # Run tests with race detector
make lint      # Run golangci-lint
make clean     # Remove build artifacts
```

## License

[MIT](./LICENSE)
