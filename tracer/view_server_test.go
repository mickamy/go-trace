package tracer_test

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/mickamy/go-trace/tracer"
)

func TestViewServer_AcceptsConnection(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sockPath := tempSocketPath(t)
	srv := tracer.NewViewServer(sockPath)

	go func() {
		if err := srv.Start(ctx); err != nil {
			t.Errorf("start: %v", err)
		}
	}()

	waitForSocket(t, sockPath)

	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", sockPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	_ = conn.Close()
}

func TestViewServer_BroadcastSingleClient(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sockPath := tempSocketPath(t)
	srv := tracer.NewViewServer(sockPath)

	go func() {
		if err := srv.Start(ctx); err != nil {
			t.Errorf("start: %v", err)
		}
	}()

	waitForSocket(t, sockPath)

	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", sockPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Allow the server to register the connection.
	time.Sleep(10 * time.Millisecond)

	now := time.Now()
	want := tracer.NewSpan("s1", "t1", "Handler", tracer.SpanKindHTTP, now, now.Add(10*time.Millisecond))
	srv.Broadcast(want)

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		t.Fatal("expected a line from server")
	}

	var got tracer.Span
	if err := json.Unmarshal(scanner.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.ID != want.ID {
		t.Errorf("ID = %q, want %q", got.ID, want.ID)
	}
	if got.Name != want.Name {
		t.Errorf("Name = %q, want %q", got.Name, want.Name)
	}
	if got.Kind != want.Kind {
		t.Errorf("Kind = %v, want %v", got.Kind, want.Kind)
	}
}

func TestViewServer_BroadcastMultipleClients(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sockPath := tempSocketPath(t)
	srv := tracer.NewViewServer(sockPath)

	go func() {
		if err := srv.Start(ctx); err != nil {
			t.Errorf("start: %v", err)
		}
	}()

	waitForSocket(t, sockPath)

	var d net.Dialer
	conn1, err := d.DialContext(ctx, "unix", sockPath)
	if err != nil {
		t.Fatalf("dial conn1: %v", err)
	}
	defer func() { _ = conn1.Close() }()

	conn2, err := d.DialContext(ctx, "unix", sockPath)
	if err != nil {
		t.Fatalf("dial conn2: %v", err)
	}
	defer func() { _ = conn2.Close() }()

	time.Sleep(10 * time.Millisecond)

	now := time.Now()
	span := tracer.NewSpan("s1", "t1", "Handler", tracer.SpanKindHTTP, now, now.Add(time.Millisecond))
	srv.Broadcast(span)

	var wg sync.WaitGroup
	wg.Add(2)

	readSpan := func(conn net.Conn, name string) {
		defer wg.Done()
		scanner := bufio.NewScanner(conn)
		if !scanner.Scan() {
			t.Errorf("%s: expected a line", name)
			return
		}
		var got tracer.Span
		if err := json.Unmarshal(scanner.Bytes(), &got); err != nil {
			t.Errorf("%s: unmarshal: %v", name, err)
			return
		}
		if got.ID != "s1" {
			t.Errorf("%s: ID = %q, want %q", name, got.ID, "s1")
		}
	}

	go readSpan(conn1, "conn1")
	go readSpan(conn2, "conn2")
	wg.Wait()
}

func TestViewServer_ClientDisconnect(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sockPath := tempSocketPath(t)
	srv := tracer.NewViewServer(sockPath)

	go func() {
		if err := srv.Start(ctx); err != nil {
			t.Errorf("start: %v", err)
		}
	}()

	waitForSocket(t, sockPath)

	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", sockPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	// Close client before broadcast.
	_ = conn.Close()

	now := time.Now()
	span := tracer.NewSpan("s1", "t1", "Handler", tracer.SpanKindHTTP, now, now.Add(time.Millisecond))

	// Should not panic or error.
	srv.Broadcast(span)
}

func TestViewServer_CleanupOnClose(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sockPath := tempSocketPath(t)
	srv := tracer.NewViewServer(sockPath)

	go func() {
		if err := srv.Start(ctx); err != nil {
			t.Errorf("start: %v", err)
		}
	}()

	waitForSocket(t, sockPath)

	if err := srv.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if _, err := os.Stat(sockPath); !os.IsNotExist(err) {
		t.Error("socket file should be removed after Close")
	}
}

func TestViewServer_ContextCancellation(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sockPath := tempSocketPath(t)
	srv := tracer.NewViewServer(sockPath)

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start(ctx)
	}()

	waitForSocket(t, sockPath)

	if err := srv.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if err := <-errCh; err != nil {
		t.Errorf("Start returned error: %v", err)
	}
}
