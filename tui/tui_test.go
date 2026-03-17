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
	end := now.Add(100 * time.Millisecond)
	root := tracer.NewSpan("root", "trace1", "Repository.ListUsers", tracer.SpanKindFunction, now, end)
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
func setupModel(t *testing.T, height int, traces ...tracer.Span) tui.Model {
	t.Helper()

	m := tui.New(nil)
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 120, Height: height})

	for _, root := range traces {
		m = updateModel(t, m, tui.TraceMsg{Root: root})
	}
	return m
}

// updateModel sends a message to the model and returns the updated model.
func updateModel(t *testing.T, m tui.Model, msg tea.Msg) tui.Model {
	t.Helper()

	updated, _ := m.Update(msg)
	model, ok := updated.(tui.Model)
	if !ok {
		t.Fatal("Update did not return tui.Model")
	}
	return model
}

// pressKey sends a key message and returns the updated model.
func pressKey(t *testing.T, m tui.Model, s string) tui.Model {
	t.Helper()
	return updateModel(t, m, keyMsg(s))
}

// pressCtrl sends a ctrl key message and returns the updated model.
func pressCtrl(t *testing.T, m tui.Model, keyType tea.KeyType) tui.Model {
	t.Helper()
	return updateModel(t, m, tea.KeyMsg{Type: keyType})
}

func TestScrollDownWithinExpandedTrace(t *testing.T) {
	t.Parallel()

	// Height 24: visible rows = 24-4 = 20.
	// One trace with 50 children = 51 lines (root + 50 children).
	m := setupModel(t, 24, makeSpanWithChildren(50))

	// Expand the trace (starts collapsed).
	m = pressKey(t, m, " ")

	// cursor should be at trace 0
	if got := m.TraceCursor(); got != 0 {
		t.Fatalf("cursor: want 0, got %d", got)
	}

	// Press j multiple times; should scroll within the trace, not move cursor.
	for range 10 {
		m = pressKey(t, m, "j")
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

	m := setupModel(t, 24, makeSpanWithChildren(50))
	m = pressKey(t, m, " ") // expand

	// Scroll down first.
	for range 10 {
		m = pressKey(t, m, "j")
	}
	scrollAfterDown := m.TraceScroll()

	// Now scroll back up.
	for range 10 {
		m = pressKey(t, m, "k")
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

	span1 := makeSpanWithChildren(5)
	span2 := makeSpanWithChildren(3)
	m := setupModel(t, 40, span1, span2)

	// Both traces are collapsed. Press j to move to next trace.
	m = pressKey(t, m, "g") // go to top
	m = pressKey(t, m, "j")

	if got := m.TraceCursor(); got != 1 {
		t.Errorf("cursor should move to 1, got %d", got)
	}
}

func TestCtrlDScrollsByHalfPage(t *testing.T) {
	t.Parallel()

	m := setupModel(t, 24, makeSpanWithChildren(50))
	m = pressKey(t, m, " ") // expand

	initialScroll := m.TraceScroll()
	m = pressCtrl(t, m, tea.KeyCtrlD)

	half := (24 - 4) / 2 // visibleRows / 2 = 10
	wantScroll := initialScroll + half

	if got := m.TraceScroll(); got != wantScroll {
		t.Errorf("traceScroll: want %d, got %d", wantScroll, got)
	}
}

func TestCtrlUScrollsUp(t *testing.T) {
	t.Parallel()

	m := setupModel(t, 24, makeSpanWithChildren(50))
	m = pressKey(t, m, " ") // expand

	// Scroll down first.
	m = pressCtrl(t, m, tea.KeyCtrlD)
	m = pressCtrl(t, m, tea.KeyCtrlD)
	scrollAfterDown := m.TraceScroll()

	// Scroll up.
	m = pressCtrl(t, m, tea.KeyCtrlU)

	half := (24 - 4) / 2
	wantScroll := scrollAfterDown - half

	if got := m.TraceScroll(); got != wantScroll {
		t.Errorf("traceScroll: want %d, got %d", wantScroll, got)
	}
}

func TestSpaceExpandAdjustsScrollForLargeTrace(t *testing.T) {
	t.Parallel()

	// Height 14: visible rows = 14-4 = 10.
	// Trace with 30 children = 31 lines (taller than viewport).
	m := setupModel(t, 14, makeSpanWithChildren(30))

	// The trace root is at line 1 (after header).
	// Expand: trace is taller than viewport, root should be at the top.
	m = pressKey(t, m, " ")

	// Root line offset is 1; traceScroll should be 1 so root is visible at the top.
	if got := m.TraceScroll(); got != 1 {
		t.Errorf("traceScroll should be 1 (root at top), got %d", got)
	}
}

func TestSpaceExpandSmallTraceShowsAllChildren(t *testing.T) {
	t.Parallel()

	// Two traces, second one has 5 children (6 lines).
	// Height 24: visible rows = 20.
	span1 := makeSpanWithChildren(2) // collapsed: 1 line
	span2 := makeSpanWithChildren(5) // expanded: 6 lines
	m := setupModel(t, 24, span1, span2)

	// Move cursor to second trace.
	m = pressKey(t, m, "j")

	// Expand second trace.
	m = pressKey(t, m, " ")

	// Both traces fit in viewport (1 collapsed + 6 expanded + 1 header = 8 lines < 20 visible).
	// Scroll should remain at 0.
	if got := m.TraceScroll(); got != 0 {
		t.Errorf("traceScroll should be 0 (no scroll needed), got %d", got)
	}
}

func TestFollowDisabledOnScrollUp(t *testing.T) {
	t.Parallel()

	m := setupModel(t, 24, makeSpanWithChildren(50))
	m = pressKey(t, m, " ") // expand

	// In follow mode initially.
	if !m.TraceFollow() {
		t.Fatal("should be in follow mode after adding trace")
	}

	// Press k to scroll up.
	m = pressKey(t, m, "k")

	if m.TraceFollow() {
		t.Errorf("follow should be disabled after pressing k")
	}
}
