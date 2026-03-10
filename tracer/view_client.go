package tracer

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
)

// ViewClient connects to a ViewServer and receives completed spans.
type ViewClient struct {
	socketPath string
}

// NewViewClient creates a client that connects to the given socket path.
func NewViewClient(socketPath string) *ViewClient {
	return &ViewClient{socketPath: socketPath}
}

// Run connects to the view server and calls onSpan for each received span.
// It blocks until the context is cancelled or the server closes the connection.
func (c *ViewClient) Run(ctx context.Context, onSpan func(traceID string, span Span)) error {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", c.socketPath)
	if err != nil {
		return fmt.Errorf("connect to view server: %w", err)
	}
	defer func() { _ = conn.Close() }()

	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, maxScanTokenSize), maxScanTokenSize)
	for scanner.Scan() {
		var span Span
		if err := json.Unmarshal(scanner.Bytes(), &span); err != nil {
			continue
		}
		onSpan(span.TraceID, span)
	}
	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		return fmt.Errorf("read from view server: %w", err)
	}

	return nil
}
