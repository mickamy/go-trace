package runtime

import (
	"context"
	"os"
	"sync"

	"github.com/mickamy/go-trace/tracer"
)

var (
	globalOnce   sync.Once
	globalTracer *Tracer
)

// GlobalTracer returns the singleton Tracer instance.
// It connects to the collector via the GOTRACE_SOCKET environment variable.
// If the variable is unset, a no-op tracer is returned so instrumented
// code can run safely without go-trace.
func GlobalTracer() *Tracer {
	globalOnce.Do(func() {
		socketPath := os.Getenv("GOTRACE_SOCKET")
		if socketPath == "" {
			globalTracer = NewTracer(noopSender{})
			return
		}
		sender, err := NewSocketSender(context.Background(), socketPath)
		if err != nil {
			globalTracer = NewTracer(noopSender{})
			return
		}
		globalTracer = NewTracer(sender)
	})
	return globalTracer
}

// Shutdown stops the global tracer and closes its sender connection.
func Shutdown() {
	if globalTracer != nil {
		globalTracer.Shutdown()
	}
}

// noopSender discards all events.
type noopSender struct{}

func (noopSender) Send(_ tracer.Event) {}
func (noopSender) Close() error        { return nil }
