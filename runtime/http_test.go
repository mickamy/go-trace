package runtime_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mickamy/go-trace/runtime"
	"github.com/mickamy/go-trace/tracer"
)

func TestMiddleware_TracesRequest(t *testing.T) {
	t.Parallel()

	rec := &recordingSender{}
	tr := runtime.NewTracer(rec)

	handler := runtime.Middleware(tr, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/users", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	events := rec.Events()
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2", len(events))
	}

	start := events[0]
	if start.Type != tracer.EventSpanStart {
		t.Errorf("events[0].Type = %v, want SpanStart", start.Type)
	}
	if start.Name != "GET /users" {
		t.Errorf("Name = %q, want %q", start.Name, "GET /users")
	}
	if start.Kind != tracer.SpanKindHTTP {
		t.Errorf("Kind = %v, want SpanKindHTTP", start.Kind)
	}

	end := events[1]
	if end.Attrs["method"] != "GET" {
		t.Errorf("Attrs[method] = %q, want %q", end.Attrs["method"], "GET")
	}
	if end.Attrs["path"] != "/users" {
		t.Errorf("Attrs[path] = %q, want %q", end.Attrs["path"], "/users")
	}
	if end.Attrs["status"] != "200" {
		t.Errorf("Attrs[status] = %q, want %q", end.Attrs["status"], "200")
	}
}

func TestMiddleware_CapturesStatusCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status int
	}{
		{"200", http.StatusOK},
		{"404", http.StatusNotFound},
		{"500", http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := &recordingSender{}
			tr := runtime.NewTracer(rec)

			handler := runtime.Middleware(tr, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
			}))

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/test", nil)
			handler.ServeHTTP(httptest.NewRecorder(), req)

			events := rec.Events()
			end := events[len(events)-1]
			want := tracer.EventSpanEnd
			if end.Type != want {
				t.Fatalf("last event Type = %v, want %v", end.Type, want)
			}
			if end.Attrs["status"] != tt.name {
				t.Errorf("Attrs[status] = %q, want %q", end.Attrs["status"], tt.name)
			}
		})
	}
}

func TestMiddleware_DefaultStatus200(t *testing.T) {
	t.Parallel()

	rec := &recordingSender{}
	tr := runtime.NewTracer(rec)

	handler := runtime.Middleware(tr, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	events := rec.Events()
	end := events[len(events)-1]
	if end.Attrs["status"] != "200" {
		t.Errorf("Attrs[status] = %q, want %q (implicit 200)", end.Attrs["status"], "200")
	}
}

func TestMiddleware_PropagatesContext(t *testing.T) {
	t.Parallel()

	rec := &recordingSender{}
	tr := runtime.NewTracer(rec)

	handler := runtime.Middleware(tr, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, finish := tr.Enter(r.Context(), "InnerFunc", tracer.SpanKindFunction)
		finish(nil)
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/login", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	events := rec.Events()
	if len(events) != 4 {
		t.Fatalf("len(events) = %d, want 4 (HTTP start/end + inner start/end)", len(events))
	}

	httpStart := events[0]
	innerStart := events[1]

	if innerStart.TraceID != httpStart.TraceID {
		t.Errorf("inner TraceID = %q, want %q (same as HTTP span)", innerStart.TraceID, httpStart.TraceID)
	}
	if innerStart.ParentID != httpStart.SpanID {
		t.Errorf("inner ParentID = %q, want %q (HTTP span ID)", innerStart.ParentID, httpStart.SpanID)
	}
}
