package tui

import (
	"fmt"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mickamy/go-trace/display"
	"github.com/mickamy/go-trace/tracer"
)

// TraceMsg is sent when a complete trace tree is ready.
type TraceMsg struct {
	Tree string
}

// AppOutputMsg is sent when the app writes to stdout/stderr.
type AppOutputMsg struct {
	Line string
}

// AppExitMsg is sent when the instrumented app exits.
type AppExitMsg struct{}

// traceEntry holds a single trace tree with collapse state.
type traceEntry struct {
	summary   string // first line (root span)
	children  string // remaining lines (child spans)
	collapsed bool
}

// lines returns the visible lines for this entry.
func (e traceEntry) lines() []string {
	if e.collapsed {
		return []string{e.summary}
	}
	if e.children == "" {
		return []string{e.summary}
	}
	return append([]string{e.summary}, strings.Split(e.children, "\n")...)
}

// Model is the bubbletea model for the go-trace TUI.
type Model struct {
	traces     []traceEntry
	appLines   []string
	cursor     int
	width      int
	height     int
	quitting   bool
	tracesOnly bool
	follow     bool

	traceScroll int
	appScroll   int
}

// New creates a new TUI model with two panes (traces + app output).
func New() Model {
	return Model{follow: true}
}

// NewTracesOnly creates a TUI model with only the traces pane.
func NewTracesOnly() Model {
	return Model{tracesOnly: true, follow: true}
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case TraceMsg:
		entry := parseTraceEntry(msg.Tree)
		entry.collapsed = entry.children != ""
		m.traces = append(m.traces, entry)
		if m.follow {
			m.cursor = len(m.traces) - 1
			m.traceScroll = m.maxTraceScroll()
		}
		return m, nil

	case AppOutputMsg:
		m.appLines = append(m.appLines, msg.Line)
		if m.follow {
			m.appScroll = m.maxAppScroll()
		}
		return m, nil

	case AppExitMsg:
		m.quitting = true
		return m, tea.Quit
	}

	return m, nil
}

func parseTraceEntry(tree string) traceEntry {
	lines := strings.SplitN(strings.TrimRight(tree, "\n"), "\n", 2)
	entry := traceEntry{summary: lines[0]}
	if len(lines) > 1 {
		entry.children = lines[1]
	}
	return entry
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		m.quitting = true
		return m, tea.Quit
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

// displayLines builds all visible lines from traces with cursor/chevron markers.
func (m Model) displayLines() []string {
	var out []string
	for i, entry := range m.traces {
		isCursor := i == m.cursor
		marker := "  "
		if isCursor {
			marker = "▶ "
		}

		hasChildren := entry.children != ""
		chevron := "  "
		if hasChildren {
			chevron = "▾ "
			if entry.collapsed {
				chevron = "▸ "
			}
		}

		summaryLine := marker + chevron + entry.summary
		if isCursor {
			summaryLine = lipgloss.NewStyle().Bold(true).Render(summaryLine)
		}
		out = append(out, summaryLine)

		if !entry.collapsed && hasChildren {
			for _, child := range strings.Split(entry.children, "\n") {
				out = append(out, "    "+child)
			}
		}
	}
	return out
}

// cursorLineOffset returns the line index where the cursor's trace starts.
func (m Model) cursorLineOffset() int {
	offset := 0
	for i := 0; i < m.cursor && i < len(m.traces); i++ {
		offset += len(m.traces[i].lines())
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
	if m.tracesOnly {
		return max(m.height-4, 3)
	}
	return max(m.height*2/3-3, 3)
}

func (m Model) appVisibleRows() int {
	return max(m.height-m.traceVisibleRows()-5, 3)
}

func (m Model) maxTraceScroll() int {
	return max(len(m.displayLines())-m.traceVisibleRows(), 0)
}

func (m Model) maxAppScroll() int {
	return max(len(m.appLines)-m.appVisibleRows(), 0)
}

func (m Model) View() string {
	if m.width == 0 {
		return "Initializing..."
	}
	if m.quitting {
		return ""
	}

	traceBox := m.renderTracePane()
	footer := m.renderFooter()

	if m.tracesOnly {
		return traceBox + "\n" + footer
	}

	appBox := m.renderAppPane()
	return traceBox + "\n" + appBox + "\n" + footer
}

func (m Model) renderTracePane() string {
	innerWidth := max(m.width-4, 20)
	visibleRows := m.traceVisibleRows()

	lines := m.displayLines()
	start := m.traceScroll
	if start > len(lines) {
		start = len(lines)
	}
	end := min(start+visibleRows, len(lines))
	visible := lines[start:end]

	for len(visible) < visibleRows {
		visible = append(visible, "")
	}

	content := strings.Join(visible, "\n")
	return m.renderBox(innerWidth, content, m.traceTitle())
}

func (m Model) renderAppPane() string {
	innerWidth := max(m.width-4, 20)
	visibleRows := m.appVisibleRows()

	start := m.appScroll
	if start > len(m.appLines) {
		start = len(m.appLines)
	}
	end := min(start+visibleRows, len(m.appLines))
	visible := m.appLines[start:end]

	for len(visible) < visibleRows {
		visible = append(visible, "")
	}

	content := strings.Join(visible, "\n")
	return m.renderBox(innerWidth, content, " App Output ")
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
		dashes := max(innerWidth-len([]rune(title)), 0)
		boxLines[0] = borderFg.Render("╭") +
			titleStyle.Render(title) +
			borderFg.Render(strings.Repeat("─", dashes)+"╮")
	}

	return strings.Join(boxLines, "\n")
}

func (m Model) renderFooter() string {
	faint := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	items := []string{
		"j/k: navigate",
		"space: collapse/expand",
		"ctrl+d/u: page",
		"G/g: bottom/top",
		"q: quit",
	}
	return faint.Render("  " + strings.Join(items, "  "))
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
// formats the tree and sends it to the TUI.
func (b *Bridge) OnSpan(traceID string, span tracer.Span) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.traces[traceID] = append(b.traces[traceID], span)

	if span.ParentID == "" {
		tree := display.FormatTree(b.traces[traceID])
		delete(b.traces, traceID)
		b.program.Send(TraceMsg{Tree: tree})
	}
}

// AppWriter is an io.Writer that sends each line to the TUI
// as an AppOutputMsg.
type AppWriter struct {
	program *tea.Program
	buf     []byte
}

// NewAppWriter creates a writer that forwards lines to the TUI.
func NewAppWriter(p *tea.Program) *AppWriter {
	return &AppWriter{program: p}
}

func (w *AppWriter) Write(data []byte) (int, error) {
	w.buf = append(w.buf, data...)

	for {
		idx := strings.IndexByte(string(w.buf), '\n')
		if idx < 0 {
			break
		}
		line := string(w.buf[:idx])
		w.buf = w.buf[idx+1:]
		w.program.Send(AppOutputMsg{Line: line})
	}

	return len(data), nil
}
