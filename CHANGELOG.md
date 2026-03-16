# Changelog

## v0.0.2

### Features
- Add analysis package for trace aggregation
- Add analytics view to TUI
- Add `-config` flag to `view` command for specifying a custom config file path

### Performance
- Defer analytics recomputation to user interaction
- Debounce analytics recomputation on incoming traces
- Defer analytics recomputation to `View()` with dirty flag
- Cache sorted analytics slices instead of sorting on every render
- Coalesce recompute ticks to avoid queue buildup under high throughput

### Fixes
- Treat `flag.ErrHelp` as success in `run` and `view` commands
- Use `ContinueOnError` for flag parsing in `run` and `view` commands
- Make IN-list SQL normalization case-insensitive
- Use comparison instead of subtraction in `sortN1`
- Add g/G keybinding hints to analytics footer

### Documentation
- Document analytics view and `matching_groups` config
- Document auto-anchoring of `matching_groups` patterns
- Document `-config` flag for `view` command

## v0.0.1

Initial release with the following features:
- CLI entry point with instrument-build-run pipeline
- AST rewriter and injector for source instrumentation
- HTTP middleware for automatic request tracing
- SQL driver wrapper for automatic query tracing
- Collector for receiving and assembling trace events via Unix socket
- Runtime package for in-app span recording and event sending
- `view` subcommand with TUI for real-time trace visualization
- ViewServer and ViewClient for broadcasting spans
- Tree display for trace visualization
- Config package for `.go-trace.yaml` loading and include pattern matching
