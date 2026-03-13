package analysis

import (
	"slices"
	"time"
)

// Stats holds aggregate timing statistics for a group of spans.
type Stats struct {
	Count int
	Total time.Duration
	Avg   time.Duration
	P95   time.Duration
	Max   time.Duration
}

// Compute calculates Stats from a slice of durations.
// Returns a zero Stats if the slice is empty.
func Compute(durations []time.Duration) Stats {
	if len(durations) == 0 {
		return Stats{}
	}

	sorted := make([]time.Duration, len(durations))
	copy(sorted, durations)
	slices.Sort(sorted)

	var total time.Duration
	for _, d := range sorted {
		total += d
	}

	return Stats{
		Count: len(sorted),
		Total: total,
		Avg:   total / time.Duration(len(sorted)),
		P95:   Percentile(sorted, 0.95),
		Max:   sorted[len(sorted)-1],
	}
}

// Percentile returns the value at the given percentile from a sorted
// slice of durations. pct must be in [0, 1]. The slice must be sorted
// in ascending order; passing an unsorted slice yields undefined results.
func Percentile(sorted []time.Duration, pct float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}

	idx := int(float64(len(sorted)-1) * pct)
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
