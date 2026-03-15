package analysis_test

import (
	"testing"

	"github.com/mickamy/go-trace/analysis"
)

func TestNormalizeSQL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple select with number",
			input: "SELECT * FROM users WHERE id = 42",
			want:  "SELECT * FROM users WHERE id = ?",
		},
		{
			name:  "single quoted string",
			input: "SELECT * FROM users WHERE name = 'Alice'",
			want:  "SELECT * FROM users WHERE name = ?",
		},
		{
			name:  "escaped single quote (O'Brien)",
			input: "SELECT * FROM users WHERE name = 'O''Brien'",
			want:  "SELECT * FROM users WHERE name = ?",
		},
		{
			name:  "double quoted identifiers preserved",
			input: `SELECT "user_id" FROM "users" WHERE "name" = 'Bob'`,
			want:  `SELECT "user_id" FROM "users" WHERE "name" = ?`,
		},
		{
			name:  "UUID",
			input: "SELECT * FROM users WHERE id = 'a1b2c3d4-e5f6-7890-abcd-ef1234567890'",
			want:  "SELECT * FROM users WHERE id = ?",
		},
		{
			name:  "hex literal",
			input: "SELECT * FROM data WHERE hash = 0xDEADBEEF",
			want:  "SELECT * FROM data WHERE hash = ?",
		},
		{
			name:  "IN list compression",
			input: "SELECT * FROM users WHERE id IN (1, 2, 3, 4, 5)",
			want:  "SELECT * FROM users WHERE id IN (?)",
		},
		{
			name:  "lowercase IN list compression",
			input: "SELECT * FROM users WHERE id in (1, 2, 3)",
			want:  "SELECT * FROM users WHERE id IN (?)",
		},
		{
			name:  "whitespace normalization",
			input: "SELECT  *  FROM   users   WHERE   id  =  1",
			want:  "SELECT * FROM users WHERE id = ?",
		},
		{
			name:  "decimal number",
			input: "SELECT * FROM items WHERE price > 19.99",
			want:  "SELECT * FROM items WHERE price > ?",
		},
		{
			name:  "multiple literals",
			input: "INSERT INTO logs (user_id, msg) VALUES (123, 'hello world')",
			want:  "INSERT INTO logs (user_id, msg) VALUES (?, ?)",
		},
		{
			name:  "empty query",
			input: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := analysis.NormalizeSQL(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeSQL(%q)\n got: %q\nwant: %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestMatchingGroups(t *testing.T) {
	t.Parallel()

	t.Run("nil returns original", func(t *testing.T) {
		t.Parallel()

		var mg *analysis.MatchingGroups
		got := mg.Match("/api/users/123")
		if got != "/api/users/123" {
			t.Errorf("got %q, want %q", got, "/api/users/123")
		}
	})

	t.Run("first match wins", func(t *testing.T) {
		t.Parallel()

		mg, err := analysis.NewMatchingGroups([]string{
			"/api/isu/.+",
			"/api/isu/[0-9]+/graph",
		})
		if err != nil {
			t.Fatal(err)
		}

		got := mg.Match("/api/isu/42/graph")
		if got != "/api/isu/.+" {
			t.Errorf("got %q, want %q", got, "/api/isu/.+")
		}
	})

	t.Run("no match returns original", func(t *testing.T) {
		t.Parallel()

		mg, err := analysis.NewMatchingGroups([]string{
			"/api/isu/.+",
		})
		if err != nil {
			t.Fatal(err)
		}

		got := mg.Match("/api/users/42")
		if got != "/api/users/42" {
			t.Errorf("got %q, want %q", got, "/api/users/42")
		}
	})

	t.Run("multiple patterns", func(t *testing.T) {
		t.Parallel()

		mg, err := analysis.NewMatchingGroups([]string{
			"/api/isu/.+",
			"/api/condition/.+",
			"/api/users/.+/icon",
		})
		if err != nil {
			t.Fatal(err)
		}

		tests := []struct {
			uri  string
			want string
		}{
			{"/api/isu/42", "/api/isu/.+"},
			{"/api/condition/abc", "/api/condition/.+"},
			{"/api/users/99/icon", "/api/users/.+/icon"},
			{"/api/health", "/api/health"},
		}

		for _, tt := range tests {
			got := mg.Match(tt.uri)
			if got != tt.want {
				t.Errorf("Match(%q) = %q, want %q", tt.uri, got, tt.want)
			}
		}
	})

	t.Run("invalid pattern returns error", func(t *testing.T) {
		t.Parallel()

		_, err := analysis.NewMatchingGroups([]string{"[invalid"})
		if err == nil {
			t.Error("expected error for invalid pattern")
		}
	})

	t.Run("auto anchor prevents partial match", func(t *testing.T) {
		t.Parallel()

		mg, err := analysis.NewMatchingGroups([]string{
			"/api/isu/.+",
		})
		if err != nil {
			t.Fatal(err)
		}

		// Should NOT match because the prefix doesn't match
		got := mg.Match("/v2/api/isu/42")
		if got != "/v2/api/isu/42" {
			t.Errorf("got %q, want %q (should not match with prefix)", got, "/v2/api/isu/42")
		}
	})
}
