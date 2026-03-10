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

// Model is the bubbletea model for the go-trace TUI.
type Model struct {
	traces     []string
	appLines   []string
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
		m.traces = append(m.traces, msg.Tree)
		if m.follow {
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

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "j", "down":
		m.traceScroll = min(m.traceScroll+1, m.maxTraceScroll())
		m.follow = m.traceScroll >= m.maxTraceScroll()
	case "k", "up":
		m.traceScroll = max(m.traceScroll-1, 0)
		m.follow = false
	case "ctrl+d":
		m.traceScroll = min(m.traceScroll+m.traceVisibleRows()/2, m.maxTraceScroll())
		m.follow = m.traceScroll >= m.maxTraceScroll()
	case "ctrl+u":
		m.traceScroll = max(m.traceScroll-m.traceVisibleRows()/2, 0)
		m.follow = false
	case "G":
		m.traceScroll = m.maxTraceScroll()
		m.follow = true
	case "g":
		m.traceScroll = 0
		m.follow = false
	}
	return m, nil
}

// traceLines returns all trace content as individual lines.
func (m Model) traceLines() []string {
	if len(m.traces) == 0 {
		return nil
	}
	return strings.Split(strings.Join(m.traces, "\n"), "\n")
}

func (m Model) traceVisibleRows() int {
	if m.tracesOnly {
		return max(m.height-4, 3) // border(2) + title line(1) + footer(1)
	}
	return max(m.height*2/3-3, 3)
}

func (m Model) appVisibleRows() int {
	return max(m.height-m.traceVisibleRows()-5, 3)
}

func (m Model) maxTraceScroll() int {
	return max(len(m.traceLines())-m.traceVisibleRows(), 0)
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

	lines := m.traceLines()
	start := m.traceScroll
	end := min(start+visibleRows, len(lines))
	if start > len(lines) {
		start = len(lines)
	}
	visible := lines[start:end]

	// Pad to fill viewport.
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
	end := min(start+visibleRows, len(m.appLines))
	if start > len(m.appLines) {
		start = len(m.appLines)
	}
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
		"j/k: scroll",
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
