package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"

	"github.com/mickamy/go-trace/tracer"
)

// Sender abstracts how events are delivered to the collector.
type Sender interface {
	Send(ev tracer.Event)
	Close() error
}

// SocketSender sends events as JSON lines over a Unix domain socket.
type SocketSender struct {
	mu   sync.Mutex
	conn net.Conn
}

// NewSocketSender connects to the collector at the given Unix socket path.
func NewSocketSender(ctx context.Context, socketPath string) (*SocketSender, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("connect to collector: %w", err)
	}
	return &SocketSender{conn: conn}, nil
}

// Send marshals the event as JSON and writes it followed by a newline.
func (s *SocketSender) Send(ev tracer.Event) {
	data, err := json.Marshal(ev)
	if err != nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	_, _ = fmt.Fprintf(s.conn, "%s\n", data)
}

// Close closes the underlying connection.
func (s *SocketSender) Close() error {
	if err := s.conn.Close(); err != nil {
		return fmt.Errorf("close socket sender: %w", err)
	}
	return nil
}
