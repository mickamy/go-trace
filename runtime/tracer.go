// Package runtime is injected into the target application at build time.
// It provides Enter/Exit functions that record trace events and send them
// to the go-trace collector over a Unix domain socket.
package runtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/mickamy/go-trace/tracer"
)

// Re-export SpanKind constants so instrumented code can reference them
// via the single runtime import.
const (
	SpanKindFunction = tracer.SpanKindFunction
	SpanKindHTTP     = tracer.SpanKindHTTP
	SpanKindSQL      = tracer.SpanKindSQL
)

// contextKey is an unexported type to prevent collisions.
type contextKey struct{}

// spanContext holds the current span info embedded in context.Context.
type spanContext struct {
	traceID string
	spanID  string
}

// Tracer records span events and sends them to the collector.
type Tracer struct {
	sender Sender

	mu      sync.Mutex
	stopped bool
}

// NewTracer creates a tracer that sends events via the given sender.
func NewTracer(sender Sender) *Tracer {
	return &Tracer{sender: sender}
}

// FinishFunc is called to end a span, optionally with attributes.
type FinishFunc func(attrs map[string]string)

// Enter starts a new span and returns a context carrying the span info.
// The returned FinishFunc must be called when the span ends.
func (t *Tracer) Enter(ctx context.Context, name string, kind tracer.SpanKind) (context.Context, FinishFunc) {
	sc := fromContext(ctx)

	spanID := generateID()
	traceID := sc.traceID
	if traceID == "" {
		traceID = generateID()
	}
	parentID := sc.spanID

	ev := tracer.NewSpanStartEvent(spanID, traceID, parentID, name, kind, time.Now())
	t.send(ev)

	newCtx := context.WithValue(ctx, contextKey{}, spanContext{
		traceID: traceID,
		spanID:  spanID,
	})

	return newCtx, func(attrs map[string]string) {
		endEv := tracer.NewSpanEndEvent(spanID, traceID, time.Now(), attrs)
		t.send(endEv)
	}
}

// Stop prevents further events from being sent.
func (t *Tracer) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.stopped = true
}

func (t *Tracer) send(ev tracer.Event) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.stopped {
		return
	}
	t.sender.Send(ev)
}

func fromContext(ctx context.Context) spanContext {
	sc, _ := ctx.Value(contextKey{}).(spanContext)
	return sc
}

func generateID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Fallback: this should never happen in practice.
		return "0000000000000000"
	}
	return hex.EncodeToString(b)
}
