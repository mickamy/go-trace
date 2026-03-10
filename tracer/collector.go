package tracer

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
)

// Collector receives trace events over a Unix domain socket
// and assembles them into complete Span trees.
type Collector struct {
	socketPath string
	listener   net.Listener

	mu       sync.Mutex
	pending  map[string]Event  // spanID -> start event
	traces   map[string][]Span // traceID -> root spans
	handlers []func(traceID string, span Span)
}

// NewCollector creates a collector that listens on the given Unix socket path.
func NewCollector(socketPath string) *Collector {
	return &Collector{
		socketPath: socketPath,
		pending:    make(map[string]Event),
		traces:     make(map[string][]Span),
	}
}

// OnSpanComplete registers a callback invoked when a span is completed.
func (c *Collector) OnSpanComplete(fn func(traceID string, span Span)) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.handlers = append(c.handlers, fn)
}

// Traces returns a snapshot of all completed root spans grouped by trace ID.
func (c *Collector) Traces() map[string][]Span {
	c.mu.Lock()
	defer c.mu.Unlock()

	out := make(map[string][]Span, len(c.traces))
	for k, v := range c.traces {
		copied := make([]Span, len(v))
		copy(copied, v)
		out[k] = copied
	}
	return out
}

// Start begins listening for events. It blocks until the context is cancelled.
func (c *Collector) Start(ctx context.Context) error {
	if err := os.RemoveAll(c.socketPath); err != nil {
		return fmt.Errorf("remove existing socket: %w", err)
	}

	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "unix", c.socketPath)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", c.socketPath, err)
	}
	c.listener = ln

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil //nolint:nilerr // expected during graceful shutdown
			}
			return fmt.Errorf("accept: %w", err)
		}
		go c.handleConn(ctx, conn)
	}
}

// SocketPath returns the path of the Unix domain socket.
func (c *Collector) SocketPath() string {
	return c.socketPath
}

// maxScanTokenSize is the maximum size for a single JSON line.
// The default bufio.Scanner limit (64 KiB) is too small for spans
// carrying large SQL queries or attribute payloads.
const maxScanTokenSize = 1 << 20 // 1 MiB

func (c *Collector) handleConn(ctx context.Context, conn net.Conn) {
	defer func() { _ = conn.Close() }()

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, maxScanTokenSize), maxScanTokenSize)
	for scanner.Scan() {
		if ctx.Err() != nil {
			return
		}

		var ev Event
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			continue
		}
		c.processEvent(ev)
	}
	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		fmt.Fprintf(os.Stderr, "collector: read error: %v\n", err)
	}
}

func (c *Collector) processEvent(ev Event) {
	c.mu.Lock()
	defer c.mu.Unlock()

	switch ev.Type {
	case EventSpanStart:
		c.pending[ev.SpanID] = ev
	case EventSpanEnd:
		start, ok := c.pending[ev.SpanID]
		if !ok {
			return
		}
		delete(c.pending, ev.SpanID)

		span := NewSpan(start.SpanID, start.TraceID, start.Name, start.Kind, start.Time, ev.Time)
		if start.ParentID != "" {
			span = span.WithParentID(start.ParentID)
		}
		for k, v := range ev.Attrs {
			span = span.WithAttr(k, v)
		}

		c.traces[span.TraceID] = append(c.traces[span.TraceID], span)

		for _, fn := range c.handlers {
			fn(span.TraceID, span)
		}
	}
}
