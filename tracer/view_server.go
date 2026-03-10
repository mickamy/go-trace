package tracer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"time"
)

// ViewServer broadcasts completed spans to connected view clients
// over a Unix domain socket using JSON lines.
type ViewServer struct {
	socketPath string

	mu       sync.Mutex
	listener net.Listener
	conns    map[net.Conn]struct{}
}

// NewViewServer creates a view server that listens on the given socket path.
func NewViewServer(socketPath string) *ViewServer {
	return &ViewServer{
		socketPath: socketPath,
		conns:      make(map[net.Conn]struct{}),
	}
}

// Start binds the socket and begins accepting connections.
// It blocks until the context is cancelled.
func (s *ViewServer) Start(ctx context.Context) error {
	if err := s.Listen(ctx); err != nil {
		return err
	}
	return s.Serve(ctx)
}

// Listen creates the Unix domain socket. Call Serve afterwards
// to begin accepting connections. This split allows callers to
// detect listen errors synchronously before launching Serve in
// a goroutine.
func (s *ViewServer) Listen(ctx context.Context) error {
	if err := os.RemoveAll(s.socketPath); err != nil {
		return fmt.Errorf("remove existing socket: %w", err)
	}

	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.socketPath, err)
	}

	s.mu.Lock()
	s.listener = ln
	s.mu.Unlock()

	return nil
}

// Serve accepts connections on the already-bound listener.
// It blocks until the context is cancelled or the listener is closed.
func (s *ViewServer) Serve(ctx context.Context) error {
	s.mu.Lock()
	ln := s.listener
	s.mu.Unlock()

	if ln == nil {
		return errors.New("listener not initialized; call Listen first")
	}

	go func() {
		<-ctx.Done()
		_ = ln.Close()
		// Close all active connections so clients unblock.
		s.mu.Lock()
		for conn := range s.conns {
			_ = conn.Close()
		}
		s.conns = make(map[net.Conn]struct{})
		s.mu.Unlock()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil //nolint:nilerr // expected during graceful shutdown
			}
			return fmt.Errorf("accept: %w", err)
		}

		s.mu.Lock()
		s.conns[conn] = struct{}{}
		s.mu.Unlock()
	}
}

// Broadcast sends a completed span to all connected view clients.
// Connections that fail to receive are removed silently.
func (s *ViewServer) Broadcast(span Span) {
	data, err := json.Marshal(span)
	if err != nil {
		return
	}
	line := make([]byte, len(data)+1)
	copy(line, data)
	line[len(data)] = '\n'

	// Snapshot connections under the lock.
	s.mu.Lock()
	snapshot := make([]net.Conn, 0, len(s.conns))
	for conn := range s.conns {
		snapshot = append(snapshot, conn)
	}
	s.mu.Unlock()

	// Write outside the lock with a deadline so a slow client
	// cannot block delivery to others.
	var failed []net.Conn
	for _, conn := range snapshot {
		_ = conn.SetWriteDeadline(time.Now().Add(broadcastWriteTimeout))
		if _, err := writeFull(conn, line); err != nil {
			_ = conn.Close()
			failed = append(failed, conn)
		}
	}

	// Remove failed connections.
	if len(failed) > 0 {
		s.mu.Lock()
		for _, conn := range failed {
			delete(s.conns, conn)
		}
		s.mu.Unlock()
	}
}

// Close shuts down the server, closes all connections, and removes the socket file.
func (s *ViewServer) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for conn := range s.conns {
		_ = conn.Close()
	}
	s.conns = make(map[net.Conn]struct{})

	if s.listener != nil {
		_ = s.listener.Close()
	}

	if err := os.RemoveAll(s.socketPath); err != nil {
		return fmt.Errorf("remove socket: %w", err)
	}
	return nil
}

// SocketPath returns the path of the Unix domain socket.
func (s *ViewServer) SocketPath() string {
	return s.socketPath
}

// ConnCount returns the number of active client connections.
func (s *ViewServer) ConnCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.conns)
}

// broadcastWriteTimeout is the maximum time allowed for writing a
// single span line to a client connection.
const broadcastWriteTimeout = 5 * time.Second

// writeFull writes all of p to w, handling short writes.
func writeFull(w net.Conn, p []byte) (int, error) {
	total := 0
	for total < len(p) {
		n, err := w.Write(p[total:])
		total += n
		if err != nil {
			return total, fmt.Errorf("write: %w", err)
		}
	}
	return total, nil
}
