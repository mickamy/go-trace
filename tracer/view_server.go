package tracer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
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

// Start begins accepting view client connections.
// It blocks until the context is cancelled.
func (s *ViewServer) Start(ctx context.Context) error {
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

	s.mu.Lock()
	defer s.mu.Unlock()

	for conn := range s.conns {
		if _, err := conn.Write(line); err != nil {
			_ = conn.Close()
			delete(s.conns, conn)
		}
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
