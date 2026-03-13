package analysis

import (
	"slices"
	"time"

	"github.com/mickamy/go-trace/tracer"
)

// SortKey determines the field used for sorting stat slices.
type SortKey int

const (
	SortByTotal SortKey = iota
	SortByAvg
	SortByP95
	SortByCount
)

// SortKeyCount is the number of available sort keys.
const SortKeyCount = 4

// String returns a human-readable label for the sort key.
func (k SortKey) String() string {
	switch k {
	case SortByTotal:
		return "total"
	case SortByAvg:
		return "avg"
	case SortByP95:
		return "p95"
	case SortByCount:
		return "count"
	default:
		return "total"
	}
}

// EndpointStat holds aggregated statistics for an HTTP endpoint.
type EndpointStat struct {
	Method string
	Path   string
	Stats
}

// SQLStat holds aggregated statistics for a normalized SQL query.
type SQLStat struct {
	Query string
	Stats
}

// FuncStat holds aggregated statistics for a function.
type FuncStat struct {
	Name string
	Stats
}

// N1Detection represents a potential N+1 query pattern.
type N1Detection struct {
	Endpoint string
	Query    string
	AvgCount float64
	MaxCount int
}

// Report contains the full analytics result.
type Report struct {
	TraceCount int
	Endpoints  []EndpointStat
	SQL        []SQLStat
	Functions  []FuncStat
	N1         []N1Detection
}

const n1Threshold = 5

// Analyze walks trace trees and produces a Report.
// mg may be nil, in which case URIs are used as-is.
func Analyze(roots []tracer.Span, mg *MatchingGroups) Report {
	endpointDurations := make(map[string][]time.Duration) // "METHOD path" -> durations
	endpointMethods := make(map[string]string)             // "METHOD path" -> method
	endpointPaths := make(map[string]string)               // "METHOD path" -> path

	sqlDurations := make(map[string][]time.Duration) // normalized query -> durations
	funcDurations := make(map[string][]time.Duration) // func name -> durations

	// N+1 detection: per-trace query counts
	type n1Key struct {
		endpoint string
		query    string
	}
	n1Counts := make(map[n1Key][]int) // (endpoint, query) -> count per trace

	for _, root := range roots {
		// Determine endpoint for this trace
		endpoint := extractEndpoint(root, mg)

		// Collect all spans in this tree
		var spans []tracer.Span
		walkSpans(root, func(s tracer.Span) {
			spans = append(spans, s)
		})

		// Per-trace query counter for N+1 detection
		traceQueryCounts := make(map[string]int)

		for _, s := range spans {
			dur := s.Duration()

			switch s.Kind {
			case tracer.SpanKindHTTP:
				method := attrOrUnknown(s, "method")
				path := attrOrUnknown(s, "path")
				if mg != nil {
					path = mg.Match(path)
				}
				key := method + " " + path
				endpointDurations[key] = append(endpointDurations[key], dur)
				endpointMethods[key] = method
				endpointPaths[key] = path

			case tracer.SpanKindSQL:
				query := attrOrUnknown(s, "query")
				if query != "(unknown)" {
					query = NormalizeSQL(query)
				}
				sqlDurations[query] = append(sqlDurations[query], dur)
				traceQueryCounts[query]++

			case tracer.SpanKindFunction:
				funcDurations[s.Name] = append(funcDurations[s.Name], dur)
			}
		}

		// Record N+1 counts for this trace
		for query, count := range traceQueryCounts {
			key := n1Key{endpoint: endpoint, query: query}
			n1Counts[key] = append(n1Counts[key], count)
		}
	}

	// Build endpoint stats
	endpoints := make([]EndpointStat, 0, len(endpointDurations))
	for key, durations := range endpointDurations {
		endpoints = append(endpoints, EndpointStat{
			Method: endpointMethods[key],
			Path:   endpointPaths[key],
			Stats:  Compute(durations),
		})
	}

	// Build SQL stats
	sqlStats := make([]SQLStat, 0, len(sqlDurations))
	for query, durations := range sqlDurations {
		sqlStats = append(sqlStats, SQLStat{
			Query: query,
			Stats: Compute(durations),
		})
	}

	// Build function stats
	funcStats := make([]FuncStat, 0, len(funcDurations))
	for name, durations := range funcDurations {
		funcStats = append(funcStats, FuncStat{
			Name:  name,
			Stats: Compute(durations),
		})
	}

	// Build N+1 detections
	var n1s []N1Detection
	for key, counts := range n1Counts {
		maxCount := 0
		totalCount := 0
		for _, c := range counts {
			totalCount += c
			if c > maxCount {
				maxCount = c
			}
		}
		if maxCount >= n1Threshold {
			n1s = append(n1s, N1Detection{
				Endpoint: key.endpoint,
				Query:    key.query,
				AvgCount: float64(totalCount) / float64(len(counts)),
				MaxCount: maxCount,
			})
		}
	}

	// Default sort by total descending
	return Report{
		TraceCount: len(roots),
		Endpoints:  SortEndpoints(endpoints, SortByTotal),
		SQL:        SortSQL(sqlStats, SortByTotal),
		Functions:  SortFunctions(funcStats, SortByTotal),
		N1:         sortN1(n1s),
	}
}

func extractEndpoint(root tracer.Span, mg *MatchingGroups) string {
	// Try to find an HTTP span in the root or immediate children
	if root.Kind == tracer.SpanKindHTTP {
		method := attrOrUnknown(root, "method")
		path := attrOrUnknown(root, "path")
		if mg != nil {
			path = mg.Match(path)
		}
		return method + " " + path
	}
	for _, child := range root.Children {
		if child.Kind == tracer.SpanKindHTTP {
			method := attrOrUnknown(child, "method")
			path := attrOrUnknown(child, "path")
			if mg != nil {
				path = mg.Match(path)
			}
			return method + " " + path
		}
	}
	return root.Name
}

func walkSpans(span tracer.Span, fn func(tracer.Span)) {
	fn(span)
	for _, child := range span.Children {
		walkSpans(child, fn)
	}
}

func attrOrUnknown(s tracer.Span, key string) string {
	if v, ok := s.Attrs[key]; ok && v != "" {
		return v
	}
	return "(unknown)"
}

// SortEndpoints returns a new slice sorted by the given key (descending).
func SortEndpoints(stats []EndpointStat, key SortKey) []EndpointStat {
	out := make([]EndpointStat, len(stats))
	copy(out, stats)
	slices.SortFunc(out, func(a, b EndpointStat) int {
		return compareStat(b.Stats, a.Stats, key) // descending
	})
	return out
}

// SortSQL returns a new slice sorted by the given key (descending).
func SortSQL(stats []SQLStat, key SortKey) []SQLStat {
	out := make([]SQLStat, len(stats))
	copy(out, stats)
	slices.SortFunc(out, func(a, b SQLStat) int {
		return compareStat(b.Stats, a.Stats, key)
	})
	return out
}

// SortFunctions returns a new slice sorted by the given key (descending).
func SortFunctions(stats []FuncStat, key SortKey) []FuncStat {
	out := make([]FuncStat, len(stats))
	copy(out, stats)
	slices.SortFunc(out, func(a, b FuncStat) int {
		return compareStat(b.Stats, a.Stats, key)
	})
	return out
}

func sortN1(n1s []N1Detection) []N1Detection {
	out := make([]N1Detection, len(n1s))
	copy(out, n1s)
	slices.SortFunc(out, func(a, b N1Detection) int {
		if a.MaxCount != b.MaxCount {
			return b.MaxCount - a.MaxCount
		}
		if a.AvgCount != b.AvgCount {
			if b.AvgCount > a.AvgCount {
				return 1
			}
			return -1
		}
		return 0
	})
	return out
}

func compareStat(a, b Stats, key SortKey) int {
	var av, bv time.Duration
	switch key {
	case SortByTotal:
		av, bv = a.Total, b.Total
	case SortByAvg:
		av, bv = a.Avg, b.Avg
	case SortByP95:
		av, bv = a.P95, b.P95
	case SortByCount:
		return a.Count - b.Count
	}
	switch {
	case av < bv:
		return -1
	case av > bv:
		return 1
	default:
		return 0
	}
}
