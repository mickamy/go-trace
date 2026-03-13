package analysis_test

import (
	"testing"
	"time"

	"github.com/mickamy/go-trace/analysis"
)

func TestPercentile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		sorted []time.Duration
		pct    float64
		want   time.Duration
	}{
		{
			name:   "empty",
			sorted: nil,
			pct:    0.95,
			want:   0,
		},
		{
			name:   "single element",
			sorted: []time.Duration{10 * time.Millisecond},
			pct:    0.95,
			want:   10 * time.Millisecond,
		},
		{
			name: "p95 of 20 elements",
			sorted: func() []time.Duration {
				ds := make([]time.Duration, 20)
				for i := range ds {
					ds[i] = time.Duration(i+1) * time.Millisecond
				}
				return ds
			}(),
			pct:  0.95,
			want: 19 * time.Millisecond, // ceil(0.95*20)=19, rank 19 → index 18 = 19ms
		},
		{
			name: "p95 of 2 elements returns max",
			sorted: []time.Duration{
				10 * time.Millisecond,
				100 * time.Millisecond,
			},
			pct:  0.95,
			want: 100 * time.Millisecond, // ceil(0.95*2)=2, rank 2 → 100ms
		},
		{
			name: "p50 median",
			sorted: func() []time.Duration {
				return []time.Duration{
					1 * time.Millisecond,
					2 * time.Millisecond,
					3 * time.Millisecond,
					4 * time.Millisecond,
					5 * time.Millisecond,
				}
			}(),
			pct:  0.50,
			want: 3 * time.Millisecond, // ceil(0.50*5)=3, rank 3 → 3ms
		},
		{
			name:   "p0 returns first",
			sorted: []time.Duration{5 * time.Millisecond, 10 * time.Millisecond},
			pct:    0.0,
			want:   5 * time.Millisecond, // ceil(0)=0, clamped to rank 1
		},
		{
			name:   "p100 returns last",
			sorted: []time.Duration{5 * time.Millisecond, 10 * time.Millisecond},
			pct:    1.0,
			want:   10 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := analysis.Percentile(tt.sorted, tt.pct)
			if got != tt.want {
				t.Errorf("Percentile(%v, %.2f) = %v, want %v", tt.sorted, tt.pct, got, tt.want)
			}
		})
	}
}

func TestCompute(t *testing.T) {
	t.Parallel()

	t.Run("empty", func(t *testing.T) {
		t.Parallel()

		got := analysis.Compute(nil)
		if got.Count != 0 {
			t.Errorf("expected Count 0, got %d", got.Count)
		}
	})

	t.Run("single element", func(t *testing.T) {
		t.Parallel()

		got := analysis.Compute([]time.Duration{42 * time.Millisecond})
		if got.Count != 1 {
			t.Errorf("Count = %d, want 1", got.Count)
		}
		if got.Total != 42*time.Millisecond {
			t.Errorf("Total = %v, want 42ms", got.Total)
		}
		if got.Avg != 42*time.Millisecond {
			t.Errorf("Avg = %v, want 42ms", got.Avg)
		}
		if got.Max != 42*time.Millisecond {
			t.Errorf("Max = %v, want 42ms", got.Max)
		}
		if got.P95 != 42*time.Millisecond {
			t.Errorf("P95 = %v, want 42ms", got.P95)
		}
	})

	t.Run("multiple elements", func(t *testing.T) {
		t.Parallel()

		durations := []time.Duration{
			50 * time.Millisecond,
			10 * time.Millisecond,
			30 * time.Millisecond,
			20 * time.Millisecond,
			40 * time.Millisecond,
		}
		got := analysis.Compute(durations)

		if got.Count != 5 {
			t.Errorf("Count = %d, want 5", got.Count)
		}
		if got.Total != 150*time.Millisecond {
			t.Errorf("Total = %v, want 150ms", got.Total)
		}
		if got.Avg != 30*time.Millisecond {
			t.Errorf("Avg = %v, want 30ms", got.Avg)
		}
		if got.Max != 50*time.Millisecond {
			t.Errorf("Max = %v, want 50ms", got.Max)
		}
	})

	t.Run("does not mutate input", func(t *testing.T) {
		t.Parallel()

		original := []time.Duration{30 * time.Millisecond, 10 * time.Millisecond, 20 * time.Millisecond}
		first := original[0]
		_ = analysis.Compute(original)
		if original[0] != first {
			t.Errorf("input was mutated: first element changed from %v to %v", first, original[0])
		}
	})
}
