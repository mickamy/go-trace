package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mickamy/go-trace/config"
)

func TestDefault_MatchesAllGoFiles(t *testing.T) {
	t.Parallel()

	cfg := config.Default()

	tests := []struct {
		path string
		want bool
	}{
		{"handler/user.go", true},
		{"internal/usecase/order.go", true},
		{"main.go", true},
		{"handler/user_test.go", true},
		{"README.md", false},
		{"go.mod", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()

			if got := cfg.Match(tt.path); got != tt.want {
				t.Errorf("Match(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestConfig_Match_IncludePatterns(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Instrument: config.InstrumentConfig{
			Include: []string{
				"**/handler/**",
				"**/usecase/**",
				"**/repository/**",
			},
		},
	}

	tests := []struct {
		path string
		want bool
	}{
		{"internal/handler/user.go", true},
		{"app/usecase/order.go", true},
		{"pkg/repository/user_repo.go", true},
		{"handler/health.go", true},
		{"cmd/api/main.go", false},
		{"pkg/util/strings.go", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()

			if got := cfg.Match(tt.path); got != tt.want {
				t.Errorf("Match(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestLoad(t *testing.T) {
	t.Parallel()

	content := `instrument:
  include:
    - "**/handler/**"
    - "**/usecase/**"
`
	dir := t.TempDir()
	path := filepath.Join(dir, ".go-trace.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if len(cfg.Instrument.Include) != 2 {
		t.Fatalf("len(Include) = %d, want 2", len(cfg.Instrument.Include))
	}

	if !cfg.Match("internal/handler/user.go") {
		t.Error("expected handler path to match")
	}
	if cfg.Match("cmd/main.go") {
		t.Error("expected cmd path not to match")
	}
}

func TestLoad_EmptyInclude_FallsBackToDefault(t *testing.T) {
	t.Parallel()

	content := `instrument: {}
`
	dir := t.TempDir()
	path := filepath.Join(dir, ".go-trace.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if !cfg.Match("anything.go") {
		t.Error("empty include should fall back to default (match all .go)")
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	t.Parallel()

	_, err := config.Load("/nonexistent/.go-trace.yaml")
	if err == nil {
		t.Error("expected error for missing file")
	}
}
