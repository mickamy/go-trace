package tui

// TraceScroll returns the trace scroll offset for testing.
func (m Model) TraceScroll() int {
	return m.traceScroll
}

// TraceCursor returns the cursor position for testing.
func (m Model) TraceCursor() int {
	return m.cursor
}

// TraceFollow returns whether follow mode is active for testing.
func (m Model) TraceFollow() bool {
	return m.follow
}
