package tracer_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/mickamy/go-trace/tracer"
)

func TestViewClient_ReceivesSpan(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sockPath := tempSocketPath(t)
	srv := tracer.NewViewServer(sockPath)

	go func() {
		if err := srv.Start(ctx); err != nil {
			t.Errorf("server start: %v", err)
		}
	}()

	waitForSocket(t, sockPath)

	var (
		mu       sync.Mutex
		received []tracer.Span
	)

	clientCtx, clientCancel := context.WithCancel(ctx)
	defer clientCancel()

	clientDone := make(chan error, 1)
	client := tracer.NewViewClient(sockPath)
	go func() {
		clientDone <- client.Run(clientCtx, func(_ string, span tracer.Span) {
			mu.Lock()
			defer mu.Unlock()
			received = append(received, span)
		})
	}()

	// Wait for client to connect.
	time.Sleep(20 * time.Millisecond)

	now := time.Now()
	want := tracer.NewSpan("s1", "t1", "Handler", tracer.SpanKindHTTP, now, now.Add(10*time.Millisecond))
	srv.Broadcast(want)

	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(received) == 1
	})

	mu.Lock()
	defer mu.Unlock()

	got := received[0]
	if got.ID != want.ID {
		t.Errorf("ID = %q, want %q", got.ID, want.ID)
	}
	if got.Name != want.Name {
		t.Errorf("Name = %q, want %q", got.Name, want.Name)
	}
	if got.TraceID != want.TraceID {
		t.Errorf("TraceID = %q, want %q", got.TraceID, want.TraceID)
	}
}

func TestViewClient_MultipleSpans(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sockPath := tempSocketPath(t)
	srv := tracer.NewViewServer(sockPath)

	go func() {
		if err := srv.Start(ctx); err != nil {
			t.Errorf("server start: %v", err)
		}
	}()

	waitForSocket(t, sockPath)

	var (
		mu       sync.Mutex
		received []tracer.Span
	)

	clientCtx, clientCancel := context.WithCancel(ctx)
	defer clientCancel()

	client := tracer.NewViewClient(sockPath)
	go func() {
		_ = client.Run(clientCtx, func(_ string, span tracer.Span) {
			mu.Lock()
			defer mu.Unlock()
			received = append(received, span)
		})
	}()

	time.Sleep(20 * time.Millisecond)

	now := time.Now()
	for i := range 3 {
		span := tracer.NewSpan("s"+string(rune('1'+i)), "t1", "Span"+string(rune('A'+i)),
			tracer.SpanKindFunction, now, now.Add(time.Millisecond))
		srv.Broadcast(span)
	}

	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(received) == 3
	})

	mu.Lock()
	defer mu.Unlock()

	names := []string{received[0].Name, received[1].Name, received[2].Name}
	if names[0] != "SpanA" || names[1] != "SpanB" || names[2] != "SpanC" {
		t.Errorf("names = %v, want [SpanA SpanB SpanC]", names)
	}
}

func TestViewClient_ServerCloses(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sockPath := tempSocketPath(t)
	srv := tracer.NewViewServer(sockPath)

	go func() {
		if err := srv.Start(ctx); err != nil {
			t.Errorf("server start: %v", err)
		}
	}()

	waitForSocket(t, sockPath)

	clientDone := make(chan error, 1)
	client := tracer.NewViewClient(sockPath)
	go func() {
		clientDone <- client.Run(ctx, func(_ string, _ tracer.Span) {})
	}()

	time.Sleep(20 * time.Millisecond)

	if err := srv.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if err := <-clientDone; err != nil {
		t.Errorf("Run should return nil on server close, got: %v", err)
	}
}

func TestViewClient_NoServer(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	client := tracer.NewViewClient("/tmp/go-trace-nonexistent.sock")

	err := client.Run(ctx, func(_ string, _ tracer.Span) {})
	if err == nil {
		t.Error("Run should return error when no server is running")
	}
}

func TestViewClient_ContextCancellation(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sockPath := tempSocketPath(t)
	srv := tracer.NewViewServer(sockPath)

	go func() {
		if err := srv.Start(ctx); err != nil {
			t.Errorf("server start: %v", err)
		}
	}()

	waitForSocket(t, sockPath)

	clientCtx, clientCancel := context.WithCancel(ctx)

	clientDone := make(chan error, 1)
	client := tracer.NewViewClient(sockPath)
	go func() {
		clientDone <- client.Run(clientCtx, func(_ string, _ tracer.Span) {})
	}()

	time.Sleep(20 * time.Millisecond)
	clientCancel()

	if err := <-clientDone; err != nil {
		t.Errorf("Run should return nil on context cancellation, got: %v", err)
	}
}
