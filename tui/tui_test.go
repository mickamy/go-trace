package tui_test

import (
	"fmt"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mickamy/go-trace/tracer"
	"github.com/mickamy/go-trace/tui"
)

// makeSpanWithChildren creates a root span with n SQL child spans.
func makeSpanWithChildren(n int) tracer.Span {
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	root := tracer.NewSpan("root", "trace1", "Repository.ListUsers", tracer.SpanKindFunction, now, now.Add(100*time.Millisecond))
	for i := range n {
		child := tracer.NewSpan(
			fmt.Sprintf("child-%d", i),
			"trace1",
			fmt.Sprintf("SQL Query %d", i),
			tracer.SpanKindSQL,
			now.Add(time.Duration(i)*time.Millisecond),
			now.Add(time.Duration(i+1)*time.Millisecond),
		).WithParentID("root")
		root = root.WithChild(child)
	}
	return root
}

// keyMsg creates a tea.KeyMsg for a regular character key.
func keyMsg(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// setupModel creates a Model with the given window size and traces already added.
func setupModel(t *testing.T, width, height int, traces ...tracer.Span) tui.Model {
	t.Helper()

	m := tui.New(nil)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	m = updated.(tui.Model)

	for _, root := range traces {
		updated, _ = m.Update(tui.TraceMsg{Root: root})
		m = updated.(tui.Model)
	}
	return m
}

// pressKey sends a key message and returns the updated model.
func pressKey(m tui.Model, s string) tui.Model {
	updated, _ := m.Update(keyMsg(s))
	return updated.(tui.Model)
}

// pressCtrl sends a ctrl key message and returns the updated model.
func pressCtrl(m tui.Model, keyType tea.KeyType) tui.Model {
	updated, _ := m.Update(tea.KeyMsg{Type: keyType})
	return updated.(tui.Model)
}

func TestScrollDownWithinExpandedTrace(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	_ = ctx

	// Height 24: visible rows = 24-4 = 20.
	// One trace with 50 children = 51 lines (root + 50 children).
	m := setupModel(t, 120, 24, makeSpanWithChildren(50))

	// Expand the trace (starts collapsed).
	m = pressKey(m, " ")

	// cursor should be at trace 0
	if got := m.TraceCursor(); got != 0 {
		t.Fatalf("cursor: want 0, got %d", got)
	}

	// Press j multiple times; should scroll within the trace, not move cursor.
	for range 10 {
		m = pressKey(m, "j")
	}

	if got := m.TraceCursor(); got != 0 {
		t.Errorf("cursor should stay at 0 after scrolling within trace, got %d", got)
	}
	if got := m.TraceScroll(); got <= 0 {
		t.Errorf("traceScroll should be > 0 after pressing j, got %d", got)
	}
}

func TestScrollUpWithinExpandedTrace(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	_ = ctx

	m := setupModel(t, 120, 24, makeSpanWithChildren(50))
	m = pressKey(m, " ") // expand

	// Scroll down first.
	for range 10 {
		m = pressKey(m, "j")
	}
	scrollAfterDown := m.TraceScroll()

	// Now scroll back up.
	for range 10 {
		m = pressKey(m, "k")
	}

	if got := m.TraceScroll(); got >= scrollAfterDown {
		t.Errorf("traceScroll should decrease after pressing k, got %d (was %d)", got, scrollAfterDown)
	}
	if got := m.TraceCursor(); got != 0 {
		t.Errorf("cursor should stay at 0, got %d", got)
	}
}

func TestJMovesToNextTraceWhenCollapsed(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	_ = ctx

	span1 := makeSpanWithChildren(5)
	span2 := makeSpanWithChildren(3)
	m := setupModel(t, 120, 40, span1, span2)

	// Both traces are collapsed. Press j to move to next trace.
	m = pressKey(m, "g") // go to top
	m = pressKey(m, "j")

	if got := m.TraceCursor(); got != 1 {
		t.Errorf("cursor should move to 1, got %d", got)
	}
}

func TestCtrlDScrollsByHalfPage(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	_ = ctx

	m := setupModel(t, 120, 24, makeSpanWithChildren(50))
	m = pressKey(m, " ") // expand

	initialScroll := m.TraceScroll()
	m = pressCtrl(m, tea.KeyCtrlD)

	half := (24 - 4) / 2 // visibleRows / 2 = 10
	wantScroll := initialScroll + half

	if got := m.TraceScroll(); got != wantScroll {
		t.Errorf("traceScroll: want %d, got %d", wantScroll, got)
	}
}

func TestCtrlUScrollsUp(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	_ = ctx

	m := setupModel(t, 120, 24, makeSpanWithChildren(50))
	m = pressKey(m, " ") // expand

	// Scroll down first.
	m = pressCtrl(m, tea.KeyCtrlD)
	m = pressCtrl(m, tea.KeyCtrlD)
	scrollAfterDown := m.TraceScroll()

	// Scroll up.
	m = pressCtrl(m, tea.KeyCtrlU)

	half := (24 - 4) / 2
	wantScroll := scrollAfterDown - half

	if got := m.TraceScroll(); got != wantScroll {
		t.Errorf("traceScroll: want %d, got %d", wantScroll, got)
	}
}

func TestSpaceExpandAdjustsScrollForLargeTrace(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	_ = ctx

	// Height 14: visible rows = 14-4 = 10.
	// Trace with 30 children = 31 lines (taller than viewport).
	m := setupModel(t, 120, 14, makeSpanWithChildren(30))

	// The trace root is at line 1 (after header).
	// Expand: trace is taller than viewport, root should be at the top.
	m = pressKey(m, " ")

	// Root line offset is 1; traceScroll should be 1 so root is visible at the top.
	if got := m.TraceScroll(); got != 1 {
		t.Errorf("traceScroll should be 1 (root at top), got %d", got)
	}
}

func TestSpaceExpandSmallTraceShowsAllChildren(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	_ = ctx

	// Two traces, second one has 5 children (6 lines).
	// Height 24: visible rows = 20.
	span1 := makeSpanWithChildren(2) // collapsed: 1 line
	span2 := makeSpanWithChildren(5) // expanded: 6 lines
	m := setupModel(t, 120, 24, span1, span2)

	// Move cursor to second trace.
	m = pressKey(m, "j")

	// Expand second trace.
	m = pressKey(m, " ")

	// Second trace should be fully visible. No scroll needed for small traces.
	if got := m.TraceScroll(); got < 0 {
		t.Errorf("traceScroll should be >= 0, got %d", got)
	}
}

func TestFollowDisabledOnScrollUp(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	_ = ctx

	m := setupModel(t, 120, 24, makeSpanWithChildren(50))
	m = pressKey(m, " ") // expand

	// In follow mode initially.
	if !m.TraceFollow() {
		t.Fatal("should be in follow mode after adding trace")
	}

	// Press k to scroll up.
	m = pressKey(m, "k")

	if m.TraceFollow() {
		t.Errorf("follow should be disabled after pressing k")
	}
}
