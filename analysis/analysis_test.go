package analysis_test

import (
	"testing"
	"time"

	"github.com/mickamy/go-trace/analysis"
	"github.com/mickamy/go-trace/tracer"
)

func makeSpan(kind tracer.SpanKind, name string, dur time.Duration, attrs map[string]string, children ...tracer.Span) tracer.Span {
	s := tracer.NewSpan("id", "trace", name, kind, time.Now(), time.Now().Add(dur))
	for k, v := range attrs {
		s = s.WithAttr(k, v)
	}
	for _, c := range children {
		s = s.WithChild(c)
	}
	return s
}

func TestAnalyze_Endpoints(t *testing.T) {
	t.Parallel()

	httpSpan := makeSpan(tracer.SpanKindHTTP, "handler", 100*time.Millisecond, map[string]string{
		"method": "GET",
		"path":   "/api/users/42",
	})
	roots := []tracer.Span{httpSpan, httpSpan}

	report := analysis.Analyze(roots, nil)

	if len(report.Endpoints) != 1 {
		t.Fatalf("expected 1 endpoint, got %d", len(report.Endpoints))
	}
	ep := report.Endpoints[0]
	if ep.Method != "GET" {
		t.Errorf("Method = %q, want %q", ep.Method, "GET")
	}
	if ep.Path != "/api/users/42" {
		t.Errorf("Path = %q, want %q", ep.Path, "/api/users/42")
	}
	if ep.Count != 2 {
		t.Errorf("Count = %d, want 2", ep.Count)
	}
}

func TestAnalyze_EndpointsWithMatchingGroups(t *testing.T) {
	t.Parallel()

	mg, err := analysis.NewMatchingGroups([]string{"/api/users/.+"})
	if err != nil {
		t.Fatal(err)
	}

	span1 := makeSpan(tracer.SpanKindHTTP, "h1", 50*time.Millisecond, map[string]string{
		"method": "GET", "path": "/api/users/42",
	})
	span2 := makeSpan(tracer.SpanKindHTTP, "h2", 80*time.Millisecond, map[string]string{
		"method": "GET", "path": "/api/users/99",
	})

	report := analysis.Analyze([]tracer.Span{span1, span2}, mg)

	if len(report.Endpoints) != 1 {
		t.Fatalf("expected 1 grouped endpoint, got %d", len(report.Endpoints))
	}
	if report.Endpoints[0].Path != "/api/users/.+" {
		t.Errorf("Path = %q, want %q", report.Endpoints[0].Path, "/api/users/.+")
	}
	if report.Endpoints[0].Count != 2 {
		t.Errorf("Count = %d, want 2", report.Endpoints[0].Count)
	}
}

func TestAnalyze_SQL(t *testing.T) {
	t.Parallel()

	sql1 := makeSpan(tracer.SpanKindSQL, "query", 10*time.Millisecond, map[string]string{
		"query": "SELECT * FROM users WHERE id = 1",
	})
	sql2 := makeSpan(tracer.SpanKindSQL, "query", 20*time.Millisecond, map[string]string{
		"query": "SELECT * FROM users WHERE id = 99",
	})

	root := makeSpan(tracer.SpanKindFunction, "main", 100*time.Millisecond, nil, sql1, sql2)
	report := analysis.Analyze([]tracer.Span{root}, nil)

	if len(report.SQL) != 1 {
		t.Fatalf("expected 1 normalized SQL group, got %d", len(report.SQL))
	}
	if report.SQL[0].Count != 2 {
		t.Errorf("Count = %d, want 2", report.SQL[0].Count)
	}
	if report.SQL[0].Query != "SELECT * FROM users WHERE id = ?" {
		t.Errorf("Query = %q, want normalized", report.SQL[0].Query)
	}
}

func TestAnalyze_Functions(t *testing.T) {
	t.Parallel()

	fn := makeSpan(tracer.SpanKindFunction, "doWork", 50*time.Millisecond, nil)
	root := makeSpan(tracer.SpanKindFunction, "main", 200*time.Millisecond, nil, fn, fn)

	report := analysis.Analyze([]tracer.Span{root}, nil)

	found := false
	for _, f := range report.Functions {
		if f.Name == "doWork" {
			found = true
			if f.Count != 2 {
				t.Errorf("doWork Count = %d, want 2", f.Count)
			}
		}
	}
	if !found {
		t.Error("expected to find doWork in function stats")
	}
}

func TestAnalyze_N1Detection(t *testing.T) {
	t.Parallel()

	httpRoot := makeSpan(tracer.SpanKindHTTP, "handler", 500*time.Millisecond, map[string]string{
		"method": "GET", "path": "/api/items",
	})

	// Add 6 identical SQL queries as children (above n1Threshold=5)
	for i := 0; i < 6; i++ {
		sql := makeSpan(tracer.SpanKindSQL, "query", 5*time.Millisecond, map[string]string{
			"query": "SELECT * FROM items WHERE id = 1",
		})
		httpRoot = httpRoot.WithChild(sql)
	}

	report := analysis.Analyze([]tracer.Span{httpRoot}, nil)

	if len(report.N1) == 0 {
		t.Fatal("expected N+1 detection, got none")
	}
	if report.N1[0].MaxCount < 6 {
		t.Errorf("MaxCount = %d, want >= 6", report.N1[0].MaxCount)
	}
}

func TestAnalyze_N1BelowThreshold(t *testing.T) {
	t.Parallel()

	root := makeSpan(tracer.SpanKindHTTP, "handler", 100*time.Millisecond, map[string]string{
		"method": "GET", "path": "/api/items",
	})

	// Only 3 queries, below threshold
	for i := 0; i < 3; i++ {
		sql := makeSpan(tracer.SpanKindSQL, "query", 5*time.Millisecond, map[string]string{
			"query": "SELECT * FROM items WHERE id = 1",
		})
		root = root.WithChild(sql)
	}

	report := analysis.Analyze([]tracer.Span{root}, nil)

	if len(report.N1) != 0 {
		t.Errorf("expected no N+1 detection for 3 queries, got %d", len(report.N1))
	}
}

func TestAnalyze_EmptyTraces(t *testing.T) {
	t.Parallel()

	report := analysis.Analyze(nil, nil)

	if report.TraceCount != 0 {
		t.Errorf("TraceCount = %d, want 0", report.TraceCount)
	}
	if len(report.Endpoints) != 0 {
		t.Errorf("expected no endpoints, got %d", len(report.Endpoints))
	}
}

func TestAnalyze_UnknownAttrs(t *testing.T) {
	t.Parallel()

	// HTTP span with no method/path attrs
	httpSpan := makeSpan(tracer.SpanKindHTTP, "handler", 50*time.Millisecond, nil)
	// SQL span with no query attr
	sqlSpan := makeSpan(tracer.SpanKindSQL, "query", 10*time.Millisecond, nil)
	root := makeSpan(tracer.SpanKindFunction, "main", 100*time.Millisecond, nil, httpSpan, sqlSpan)

	report := analysis.Analyze([]tracer.Span{root}, nil)

	if len(report.Endpoints) != 1 || report.Endpoints[0].Method != "(unknown)" {
		t.Errorf("expected (unknown) method endpoint")
	}
	if len(report.SQL) != 1 || report.SQL[0].Query != "(unknown)" {
		t.Errorf("expected (unknown) SQL query")
	}
}

func TestSortEndpoints(t *testing.T) {
	t.Parallel()

	endpoints := []analysis.EndpointStat{
		{Method: "GET", Path: "/a", Stats: analysis.Stats{Count: 10, Total: 100 * time.Millisecond}},
		{Method: "POST", Path: "/b", Stats: analysis.Stats{Count: 5, Total: 500 * time.Millisecond}},
		{Method: "GET", Path: "/c", Stats: analysis.Stats{Count: 20, Total: 200 * time.Millisecond}},
	}

	byTotal := analysis.SortEndpoints(endpoints, analysis.SortByTotal)
	if byTotal[0].Path != "/b" {
		t.Errorf("expected /b first by total, got %s", byTotal[0].Path)
	}

	byCount := analysis.SortEndpoints(endpoints, analysis.SortByCount)
	if byCount[0].Path != "/c" {
		t.Errorf("expected /c first by count, got %s", byCount[0].Path)
	}

	// Verify original is not mutated
	if endpoints[0].Path != "/a" {
		t.Error("original slice was mutated")
	}
}

func TestSortKeyString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		key  analysis.SortKey
		want string
	}{
		{analysis.SortByTotal, "total"},
		{analysis.SortByAvg, "avg"},
		{analysis.SortByP95, "p95"},
		{analysis.SortByCount, "count"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			if got := tt.key.String(); got != tt.want {
				t.Errorf("SortKey(%d).String() = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}
