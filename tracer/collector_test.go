package tracer_test

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/mickamy/go-trace/tracer"
)

func tempSocketPath(t *testing.T) string {
	t.Helper()
	// Use /tmp directly to avoid macOS 104-byte Unix socket path limit.
	// t.TempDir() produces paths too long for Unix sockets.
	f, err := os.CreateTemp("/tmp", "gt-*.sock")
	if err != nil {
		t.Fatalf("create temp socket: %v", err)
	}
	path := f.Name()
	_ = f.Close()
	_ = os.Remove(path)
	t.Cleanup(func() { _ = os.Remove(path) })
	return path
}

func sendEvents(t *testing.T, socketPath string, events []tracer.Event) {
	t.Helper()

	var d net.Dialer
	conn, err := d.DialContext(t.Context(), "unix", socketPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	for _, ev := range events {
		data, err := json.Marshal(ev)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		fmt.Fprintf(conn, "%s\n", data)
	}
}

func TestCollector_SingleSpan(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sockPath := tempSocketPath(t)
	col := tracer.NewCollector(sockPath)

	var (
		mu        sync.Mutex
		completed []tracer.Span
	)
	col.OnSpanComplete(func(_ string, span tracer.Span) {
		mu.Lock()
		defer mu.Unlock()
		completed = append(completed, span)
	})

	go func() {
		if err := col.Start(ctx); err != nil {
			t.Errorf("collector start: %v", err)
		}
	}()

	waitForSocket(t, sockPath)

	now := time.Now()
	sendEvents(t, sockPath, []tracer.Event{
		tracer.NewSpanStartEvent("s1", "t1", "", "Handler", tracer.SpanKindHTTP, now),
		tracer.NewSpanEndEvent("s1", "t1", now.Add(10*time.Millisecond), nil),
	})

	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(completed) == 1
	})

	mu.Lock()
	defer mu.Unlock()

	span := completed[0]
	if span.ID != "s1" {
		t.Errorf("ID = %q, want %q", span.ID, "s1")
	}
	if span.Name != "Handler" {
		t.Errorf("Name = %q, want %q", span.Name, "Handler")
	}
	if span.Kind != tracer.SpanKindHTTP {
		t.Errorf("Kind = %v, want %v", span.Kind, tracer.SpanKindHTTP)
	}
	if span.Duration() != 10*time.Millisecond {
		t.Errorf("Duration = %v, want %v", span.Duration(), 10*time.Millisecond)
	}
}

func TestCollector_SpanWithAttrs(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sockPath := tempSocketPath(t)
	col := tracer.NewCollector(sockPath)

	var (
		mu        sync.Mutex
		completed []tracer.Span
	)
	col.OnSpanComplete(func(_ string, span tracer.Span) {
		mu.Lock()
		defer mu.Unlock()
		completed = append(completed, span)
	})

	go func() {
		if err := col.Start(ctx); err != nil {
			t.Errorf("collector start: %v", err)
		}
	}()

	waitForSocket(t, sockPath)

	now := time.Now()
	sendEvents(t, sockPath, []tracer.Event{
		tracer.NewSpanStartEvent("s1", "t1", "", "Query", tracer.SpanKindSQL, now),
		tracer.NewSpanEndEvent("s1", "t1", now.Add(5*time.Millisecond), map[string]string{
			"query": "SELECT 1",
			"rows":  "1",
		}),
	})

	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(completed) == 1
	})

	mu.Lock()
	defer mu.Unlock()

	if completed[0].Attrs["query"] != "SELECT 1" {
		t.Errorf("Attrs[query] = %q, want %q", completed[0].Attrs["query"], "SELECT 1")
	}
	if completed[0].Attrs["rows"] != "1" {
		t.Errorf("Attrs[rows] = %q, want %q", completed[0].Attrs["rows"], "1")
	}
}

func TestCollector_Traces(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sockPath := tempSocketPath(t)
	col := tracer.NewCollector(sockPath)

	go func() {
		if err := col.Start(ctx); err != nil {
			t.Errorf("collector start: %v", err)
		}
	}()

	waitForSocket(t, sockPath)

	now := time.Now()
	sendEvents(t, sockPath, []tracer.Event{
		tracer.NewSpanStartEvent("s1", "t1", "", "A", tracer.SpanKindFunction, now),
		tracer.NewSpanEndEvent("s1", "t1", now.Add(time.Millisecond), nil),
		tracer.NewSpanStartEvent("s2", "t2", "", "B", tracer.SpanKindFunction, now),
		tracer.NewSpanEndEvent("s2", "t2", now.Add(2*time.Millisecond), nil),
	})

	waitFor(t, func() bool {
		traces := col.Traces()
		return len(traces["t1"]) == 1 && len(traces["t2"]) == 1
	})

	traces := col.Traces()
	if traces["t1"][0].Name != "A" {
		t.Errorf("traces[t1][0].Name = %q, want %q", traces["t1"][0].Name, "A")
	}
	if traces["t2"][0].Name != "B" {
		t.Errorf("traces[t2][0].Name = %q, want %q", traces["t2"][0].Name, "B")
	}
}

func TestCollector_TracesReturnsSnapshot(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sockPath := tempSocketPath(t)
	col := tracer.NewCollector(sockPath)

	go func() {
		if err := col.Start(ctx); err != nil {
			t.Errorf("collector start: %v", err)
		}
	}()

	waitForSocket(t, sockPath)

	now := time.Now()
	sendEvents(t, sockPath, []tracer.Event{
		tracer.NewSpanStartEvent("s1", "t1", "", "A", tracer.SpanKindFunction, now),
		tracer.NewSpanEndEvent("s1", "t1", now.Add(time.Millisecond), nil),
	})

	waitFor(t, func() bool {
		return len(col.Traces()["t1"]) == 1
	})

	snapshot := col.Traces()
	snapshot["t1"] = nil

	if len(col.Traces()["t1"]) != 1 {
		t.Error("Traces() did not return a snapshot; mutation affected internal state")
	}
}

func TestCollector_OrphanEndEventIgnored(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sockPath := tempSocketPath(t)
	col := tracer.NewCollector(sockPath)

	var (
		mu    sync.Mutex
		count int
	)
	col.OnSpanComplete(func(_ string, _ tracer.Span) {
		mu.Lock()
		defer mu.Unlock()
		count++
	})

	go func() {
		if err := col.Start(ctx); err != nil {
			t.Errorf("collector start: %v", err)
		}
	}()

	waitForSocket(t, sockPath)

	sendEvents(t, sockPath, []tracer.Event{
		tracer.NewSpanEndEvent("orphan", "t1", time.Now(), nil),
	})

	// Give time for event processing, then verify no spans completed.
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if count != 0 {
		t.Errorf("completed count = %d, want 0 (orphan end event should be ignored)", count)
	}
}

func waitForSocket(t *testing.T, path string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("socket %s did not appear", path)
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}
