package runtime

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/mickamy/go-trace/tracer"
)

// Middleware returns an http.Handler that traces each request as a span.
func Middleware(t *Tracer, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := fmt.Sprintf("%s %s", r.Method, r.URL.Path)
		ctx, finish := t.Enter(r.Context(), name, tracer.SpanKindHTTP)

		rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r.WithContext(ctx))

		finish(map[string]string{
			"method": r.Method,
			"path":   r.URL.Path,
			"status": strconv.Itoa(rw.status),
		})
	})
}

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter

	status      int
	wroteHeader bool
}

func (rw *responseWriter) WriteHeader(code int) {
	if !rw.wroteHeader {
		rw.status = code
		rw.wroteHeader = true
	}
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.wroteHeader {
		rw.wroteHeader = true
	}
	return rw.ResponseWriter.Write(b)
}
