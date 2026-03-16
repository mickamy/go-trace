package analysis

import (
	"fmt"
	"regexp"
	"strings"
)

// SQL normalization patterns.
var (
	// Single-quoted string literals: 'foo', 'O''Brien'
	// Double-quoted identifiers ("table_name") are intentionally preserved
	// because they are identifiers in Postgres/ANSI SQL, not literals.
	reSingleQuoted = regexp.MustCompile(`'(?:[^'\\]|\\.|\'{2})*'`)

	// Numbers: integers and decimals (not preceded by a letter/underscore)
	reNumber = regexp.MustCompile(`\b\d+(?:\.\d+)?\b`)

	// UUIDs: 8-4-4-4-12 hex pattern
	reUUID = regexp.MustCompile(`\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b`)

	// Hex strings: 0x-prefixed (e.g. 0xDEADBEEF)
	reHex = regexp.MustCompile(`\b0x[0-9a-fA-F]+\b`)

	// IN list compression: IN (?, ?, ..., ?) -> IN (?) (case-insensitive)
	reINList = regexp.MustCompile(`(?i)\bIN\s*\(\s*\?(?:\s*,\s*\?)*\s*\)`)

	// Whitespace normalization
	reWhitespace = regexp.MustCompile(`\s+`)
)

// NormalizeSQL replaces literals in a SQL query with ? placeholders,
// compresses IN lists, and normalizes whitespace.
func NormalizeSQL(query string) string {
	q := reUUID.ReplaceAllString(query, "?")
	q = reHex.ReplaceAllString(q, "?")
	q = reSingleQuoted.ReplaceAllString(q, "?")
	q = reNumber.ReplaceAllString(q, "?")
	q = reINList.ReplaceAllString(q, "IN (?)")
	q = reWhitespace.ReplaceAllString(q, " ")
	return strings.TrimSpace(q)
}

// MatchingGroups groups URIs using regex patterns, like alp's --matching-groups.
// The first matching pattern's string becomes the group key.
type MatchingGroups struct {
	patterns  []*regexp.Regexp
	originals []string
}

// NewMatchingGroups compiles pattern strings into a MatchingGroups.
// Each pattern must be a valid regular expression. Patterns are
// automatically anchored with ^ and $ if not already present.
func NewMatchingGroups(patterns []string) (*MatchingGroups, error) {
	mg := &MatchingGroups{
		patterns:  make([]*regexp.Regexp, len(patterns)),
		originals: make([]string, len(patterns)),
	}
	for i, p := range patterns {
		anchored := p
		if !strings.HasPrefix(anchored, "^") {
			anchored = "^" + anchored
		}
		if !strings.HasSuffix(anchored, "$") {
			anchored += "$"
		}
		re, err := regexp.Compile(anchored)
		if err != nil {
			return nil, fmt.Errorf("compile pattern %q: %w", p, err)
		}
		mg.patterns[i] = re
		mg.originals[i] = p
	}
	return mg, nil
}

// Match returns the first matching pattern string for the given URI.
// If no pattern matches, the original URI is returned unchanged.
func (mg *MatchingGroups) Match(uri string) string {
	if mg == nil {
		return uri
	}
	for i, re := range mg.patterns {
		if re.MatchString(uri) {
			return mg.originals[i]
		}
	}
	return uri
}
