package runtime

import (
	"context"
	"os"
	"sync"

	"github.com/mickamy/go-trace/tracer"
)

var (
	globalMu     sync.Mutex
	globalOnce   sync.Once
	globalTracer *Tracer
)

// GlobalTracer returns the singleton Tracer instance.
// It connects to the collector via the GOTRACE_SOCKET environment variable.
// If the variable is unset, a no-op tracer is returned so instrumented
// code can run safely without go-trace.
func GlobalTracer() *Tracer {
	globalOnce.Do(func() {
		var t *Tracer

		socketPath := os.Getenv("GOTRACE_SOCKET")
		if socketPath == "" {
			t = NewTracer(noopSender{})
		} else {
			sender, err := NewSocketSender(context.Background(), socketPath)
			if err != nil {
				t = NewTracer(noopSender{})
			} else {
				t = NewTracer(sender)
			}
		}

		globalMu.Lock()
		globalTracer = t
		globalMu.Unlock()
	})

	globalMu.Lock()
	defer globalMu.Unlock()
	return globalTracer
}

// Shutdown stops the global tracer and closes its sender connection.
// Safe to call concurrently with GlobalTracer.
func Shutdown() {
	globalMu.Lock()
	t := globalTracer
	globalMu.Unlock()

	if t != nil {
		t.Shutdown()
	}
}

// noopSender discards all events.
type noopSender struct{}

func (noopSender) Send(_ tracer.Event) {}
func (noopSender) Close() error        { return nil }
