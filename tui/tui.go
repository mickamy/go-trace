package tui

import (
	"fmt"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mickamy/go-trace/analysis"
	"github.com/mickamy/go-trace/display"
	"github.com/mickamy/go-trace/tracer"
)

// TraceMsg is sent when a complete trace tree is ready.
type TraceMsg struct {
	Root tracer.Span
}

// AppExitMsg is sent when the trace source disconnects.
type AppExitMsg struct{}

// ErrorMsg is sent when the view client encounters an error.
type ErrorMsg struct {
	Err error
}

// Column widths.
const (
	colMarker   = 4  // "▶ " + "▾ "
	colKind     = 10 // "function" + padding
	colDuration = 12
	colTime     = 12 // "15:04:05.000"
)

// traceEntry holds a single trace tree with collapse state.
type traceEntry struct {
	root      tracer.Span
	collapsed bool
}

// lineCount returns the number of display lines for this entry.
func (e traceEntry) lineCount() int {
	if e.collapsed {
		return 1
	}
	return countSpanLines(e.root)
}

func countSpanLines(span tracer.Span) int {
	n := 1
	for _, child := range span.Children {
		n += countSpanLines(child)
	}
	return n
}

// viewMode represents whether the TUI shows traces or analytics.
type viewMode int

const (
	viewTrace viewMode = iota
	viewAnalytics
)

// Model is the bubbletea model for the go-trace TUI.
type Model struct {
	traces   []traceEntry
	cursor   int
	width    int
	height   int
	quitting bool
	follow   bool
	err      error

	traceScroll int

	// Analytics view state
	mode            viewMode
	report          analysis.Report
	reportDirty     bool // true when traces changed since last report computation
	matchingGroups  *analysis.MatchingGroups
	analyticsTab    analyticsTab
	analyticsSort   analysis.SortKey
	analyticsCursor int
	analyticsScroll int
}

// New creates a new TUI model.
func New(mg *analysis.MatchingGroups) Model {
	return Model{
		follow:         true,
		matchingGroups: mg,
	}
}

// Err returns the error that caused the TUI to exit, if any.
func (m Model) Err() error {
	return m.err
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case TraceMsg:
		entry := traceEntry{
			root:      msg.Root,
			collapsed: len(msg.Root.Children) > 0,
		}
		m.traces = append(m.traces, entry)
		if m.follow {
			m.cursor = len(m.traces) - 1
			m.traceScroll = m.maxTraceScroll()
		}
		m.reportDirty = true
		return m, nil

	case ErrorMsg:
		m.err = msg.Err
		m.quitting = true
		return m, tea.Quit

	case AppExitMsg:
		m.quitting = true
		return m, tea.Quit
	}

	return m, nil
}

// View implements tea.Model.
func (m Model) View() string {
	if m.width == 0 {
		return "Initializing..."
	}
	if m.quitting {
		if m.err != nil {
			return fmt.Sprintf("error: %v\n", m.err)
		}
		return ""
	}

	if m.mode == viewAnalytics && m.reportDirty {
		m = m.recomputeReport()
	}

	switch m.mode {
	case viewTrace:
		return m.renderTracePane() + "\n" + m.renderFooter()
	case viewAnalytics:
		return m.renderAnalyticsPane() + "\n" + m.renderAnalyticsFooter()
	}

	return m.renderTracePane() + "\n" + m.renderFooter()
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	}

	if m.mode == viewAnalytics {
		return m.handleAnalyticsKey(msg)
	}
	return m.handleTraceKey(msg)
}

func (m Model) handleTraceKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "a":
		m.mode = viewAnalytics
		m = m.recomputeReport()
		m.analyticsCursor = 0
		m.analyticsScroll = 0
	case "j", "down":
		if m.cursor < len(m.traces)-1 {
			m.cursor++
		}
		m.follow = m.cursor >= len(m.traces)-1
		m = m.ensureCursorVisible()
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
		m.follow = false
		m = m.ensureCursorVisible()
	case "ctrl+d":
		m.cursor = min(m.cursor+m.traceVisibleRows()/2, max(len(m.traces)-1, 0))
		m.follow = m.cursor >= len(m.traces)-1
		m = m.ensureCursorVisible()
	case "ctrl+u":
		m.cursor = max(m.cursor-m.traceVisibleRows()/2, 0)
		m.follow = false
		m = m.ensureCursorVisible()
	case "G":
		m.cursor = max(len(m.traces)-1, 0)
		m.traceScroll = m.maxTraceScroll()
		m.follow = true
	case "g":
		m.cursor = 0
		m.traceScroll = 0
		m.follow = false
	case " ":
		if m.cursor >= 0 && m.cursor < len(m.traces) {
			m.traces[m.cursor].collapsed = !m.traces[m.cursor].collapsed
			m = m.clampScroll()
		}
	}
	return m, nil
}

func (m Model) handleAnalyticsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = viewTrace
	case "tab":
		m.analyticsTab = (m.analyticsTab + 1) % analyticsTabCount
		m.analyticsCursor = 0
		m.analyticsScroll = 0
	case "shift+tab":
		m.analyticsTab = (m.analyticsTab + analyticsTabCount - 1) % analyticsTabCount
		m.analyticsCursor = 0
		m.analyticsScroll = 0
	case "s":
		m.analyticsSort = (m.analyticsSort + 1) % analysis.SortKeyCount
	case "j", "down":
		itemCount := m.analyticsItemCount()
		if m.analyticsCursor < itemCount-1 {
			m.analyticsCursor++
		}
		m = m.ensureAnalyticsCursorVisible()
	case "k", "up":
		if m.analyticsCursor > 0 {
			m.analyticsCursor--
		}
		m = m.ensureAnalyticsCursorVisible()
	case "G":
		m.analyticsCursor = max(m.analyticsItemCount()-1, 0)
		m.analyticsScroll = m.maxAnalyticsScroll()
	case "g":
		m.analyticsCursor = 0
		m.analyticsScroll = 0
	}
	return m, nil
}

func (m Model) recomputeReport() Model {
	roots := make([]tracer.Span, len(m.traces))
	for i, entry := range m.traces {
		roots[i] = entry.root
	}
	m.report = analysis.Analyze(roots, m.matchingGroups)
	m.reportDirty = false
	return m
}

func (m Model) ensureAnalyticsCursorVisible() Model {
	// The cursor row offset is: 2 (tab bar + blank) + 2 (sort label + header) + cursorIndex
	offset := 4 + m.analyticsCursor
	visibleRows := m.analyticsVisibleRows()

	if offset < m.analyticsScroll {
		m.analyticsScroll = offset
	}
	if offset >= m.analyticsScroll+visibleRows {
		m.analyticsScroll = offset - visibleRows + 1
	}
	m.analyticsScroll = min(m.analyticsScroll, m.maxAnalyticsScroll())
	m.analyticsScroll = max(m.analyticsScroll, 0)
	return m
}

// displayLines builds all visible lines with header, cursor/chevron and columns.
func (m Model) displayLines() []string {
	innerWidth := max(m.width-4, 20)
	colName := max(innerWidth-colMarker-colKind-colDuration-colTime-3, 10) // 3 = gaps

	header := fmt.Sprintf("    %-*s %-*s %*s %*s",
		colKind, "Kind",
		colName, "Name",
		colDuration, "Duration",
		colTime, "Time",
	)
	out := []string{lipgloss.NewStyle().Bold(true).Render(header)}

	for i, entry := range m.traces {
		isCursor := i == m.cursor
		lines := m.renderSpanRows(entry, isCursor, colName)
		out = append(out, lines...)
	}
	return out
}

func (m Model) renderSpanRows(entry traceEntry, isCursor bool, colName int) []string {
	marker := "  "
	if isCursor {
		marker = "▶ "
	}

	hasChildren := len(entry.root.Children) > 0
	chevron := "  "
	if hasChildren {
		chevron = "▾ "
		if entry.collapsed {
			chevron = "▸ "
		}
	}

	rootLine := m.formatSpanRow(marker, chevron, "", entry.root, colName, isCursor)
	if entry.collapsed {
		return []string{rootLine}
	}

	lines := make([]string, 1, entry.lineCount())
	lines[0] = rootLine
	for i, child := range entry.root.Children {
		isLast := i == len(entry.root.Children)-1
		lines = append(lines, m.renderChildRows(child, "    ", isLast, colName)...)
	}
	return lines
}

func (m Model) renderChildRows(span tracer.Span, prefix string, isLast bool, colName int) []string {
	connector := "├── "
	if isLast {
		connector = "└── "
	}

	treePrefix := prefix + connector
	line := m.formatSpanRow("  ", "  ", treePrefix, span, colName, false)

	lines := make([]string, 1, countSpanLines(span))
	lines[0] = line
	childPrefix := prefix + "│   "
	if isLast {
		childPrefix = prefix + "    "
	}
	for i, child := range span.Children {
		childIsLast := i == len(span.Children)-1
		lines = append(lines, m.renderChildRows(child, childPrefix, childIsLast, colName)...)
	}
	return lines
}

func (m Model) formatSpanRow(marker, chevron, treePrefix string, span tracer.Span, colName int, bold bool) string {
	name := treePrefix + span.Name
	name = truncate(name, colName)

	kind := span.Kind.String()
	dur := formatDuration(span.Duration())
	t := formatTime(span.StartTime)

	kindStyled := lipgloss.NewStyle().
		Foreground(kindColor(span.Kind)).
		Render(kind)

	if bold {
		b := lipgloss.NewStyle().Bold(true)
		return b.Render(marker+chevron) +
			padRight(kindStyled, colKind) + " " +
			padRight(b.Render(name), colName) + " " +
			padLeft(b.Render(dur), colDuration) + " " +
			padLeft(b.Render(t), colTime)
	}

	return marker + chevron +
		padRight(kindStyled, colKind) + " " +
		padRight(name, colName) + " " +
		padLeft(dur, colDuration) + " " +
		padLeft(t, colTime)
}

// cursorLineOffset returns the line index where the cursor's trace starts,
// including the header row.
func (m Model) cursorLineOffset() int {
	offset := 1 // header row
	for i := 0; i < m.cursor && i < len(m.traces); i++ {
		offset += m.traces[i].lineCount()
	}
	return offset
}

func (m Model) ensureCursorVisible() Model {
	offset := m.cursorLineOffset()
	visibleRows := m.traceVisibleRows()

	if offset < m.traceScroll {
		m.traceScroll = offset
	}
	if offset >= m.traceScroll+visibleRows {
		m.traceScroll = offset - visibleRows + 1
	}
	return m.clampScroll()
}

func (m Model) clampScroll() Model {
	m.traceScroll = min(m.traceScroll, m.maxTraceScroll())
	m.traceScroll = max(m.traceScroll, 0)
	return m
}

func (m Model) traceVisibleRows() int {
	return max(m.height-4, 3) // border(2) + title(1) + footer(1); header is part of displayLines
}

func (m Model) maxTraceScroll() int {
	totalLines := 1 // header row
	for _, entry := range m.traces {
		totalLines += entry.lineCount()
	}
	return max(totalLines-m.traceVisibleRows(), 0)
}

func (m Model) renderTracePane() string {
	innerWidth := max(m.width-4, 20)
	visibleRows := m.traceVisibleRows()

	lines := m.displayLines()
	start := min(m.traceScroll, len(lines))
	end := min(start+visibleRows, len(lines))
	visible := lines[start:end]

	for len(visible) < visibleRows {
		visible = append(visible, "")
	}

	content := strings.Join(visible, "\n")
	return m.renderBox(innerWidth, content, m.traceTitle())
}

func (m Model) traceTitle() string {
	n := len(m.traces)
	title := fmt.Sprintf(" go-trace (%d traces) ", n)
	if m.follow {
		title += "[following] "
	}
	return title
}

func (m Model) renderBox(innerWidth int, content, title string) string {
	borderColor := lipgloss.Color("240")
	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Width(innerWidth).
		BorderForeground(borderColor)

	box := border.Render(content)
	boxLines := strings.Split(box, "\n")

	if len(boxLines) > 0 {
		borderFg := lipgloss.NewStyle().Foreground(borderColor)
		titleStyle := lipgloss.NewStyle().Bold(true)
		titleRunes := []rune(title)
		if len(titleRunes) > innerWidth-2 { // 2 = border corners
			titleRunes = titleRunes[:max(innerWidth-2, 0)]
		}
		dashes := max(innerWidth-len(titleRunes), 0)
		boxLines[0] = borderFg.Render("╭") +
			titleStyle.Render(string(titleRunes)) +
			borderFg.Render(strings.Repeat("─", dashes)+"╮")
	}

	return strings.Join(boxLines, "\n")
}

func (m Model) renderFooter() string {
	faint := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	items := []string{
		"a: analytics",
		"j/k: navigate",
		"space: collapse/expand",
		"ctrl+d/u: page",
		"G/g: bottom/top",
		"q: quit",
	}
	return faint.Render("  " + strings.Join(items, "  "))
}

func kindColor(kind tracer.SpanKind) lipgloss.Color {
	switch kind {
	case tracer.SpanKindHTTP:
		return lipgloss.Color("6") // cyan
	case tracer.SpanKindSQL:
		return lipgloss.Color("3") // yellow
	case tracer.SpanKindFunction:
		return lipgloss.Color("5") // magenta
	default:
		return lipgloss.Color("7")
	}
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Local().Format("15:04:05.000") //nolint:gosmopolitan // TUI displays local time
}

func formatDuration(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return fmt.Sprintf("%.0fµs", float64(d.Microseconds()))
	case d < time.Second:
		return fmt.Sprintf("%.1fms", float64(d.Microseconds())/1000)
	default:
		return fmt.Sprintf("%.2fs", d.Seconds())
	}
}

func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen <= 1 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-1]) + "…"
}

func padRight(s string, width int) string {
	w := lipgloss.Width(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

func padLeft(s string, width int) string { //nolint:unparam // width varies by column
	w := lipgloss.Width(s)
	if w >= width {
		return s
	}
	return strings.Repeat(" ", width-w) + s
}

// Bridge connects the tracer collector to the TUI via bubbletea messages.
type Bridge struct {
	program *tea.Program

	mu     sync.Mutex
	traces map[string][]tracer.Span
}

// NewBridge creates a bridge that sends trace trees to the TUI program.
func NewBridge(p *tea.Program) *Bridge {
	return &Bridge{
		program: p,
		traces:  make(map[string][]tracer.Span),
	}
}

// OnSpan handles a completed span. When the root arrives,
// builds the tree and sends it to the TUI.
func (b *Bridge) OnSpan(traceID string, span tracer.Span) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.traces[traceID] = append(b.traces[traceID], span)

	if span.ParentID == "" {
		root := display.BuildTree(b.traces[traceID])
		delete(b.traces, traceID)
		b.program.Send(TraceMsg{Root: root})
	}
}
