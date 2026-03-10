package tui

import (
	"strings"
	"sync"

	"github.com/charmbracelet/bubbles/viewport"
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
	traceView  viewport.Model
	appView    viewport.Model
	traces     []string
	appLines   []string
	width      int
	height     int
	ready      bool
	quitting   bool
	tracesOnly bool
}

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205")).
			PaddingLeft(1)

	borderStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62"))
)

// New creates a new TUI model with two panes (traces + app output).
func New() Model {
	return Model{}
}

// NewTracesOnly creates a TUI model with only the traces pane.
func NewTracesOnly() Model {
	return Model{tracesOnly: true}
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m = m.updateLayout()
		m.ready = true
		return m, nil

	case TraceMsg:
		m.traces = append(m.traces, msg.Tree)
		m.traceView.SetContent(strings.Join(m.traces, "\n"))
		m.traceView.GotoBottom()
		return m, nil

	case AppOutputMsg:
		m.appLines = append(m.appLines, msg.Line)
		m.appView.SetContent(strings.Join(m.appLines, "\n"))
		m.appView.GotoBottom()
		return m, nil

	case AppExitMsg:
		m.quitting = true
		return m, tea.Quit
	}

	var cmd tea.Cmd
	m.traceView, cmd = m.traceView.Update(msg)
	return m, cmd
}

func (m Model) updateLayout() Model {
	contentWidth := m.width - 2 // border

	if m.tracesOnly {
		traceHeight := m.height - 4
		if traceHeight < 3 {
			traceHeight = 3
		}
		m.traceView = viewport.New(contentWidth, traceHeight)
		m.traceView.SetContent(strings.Join(m.traces, "\n"))
		return m
	}

	traceHeight := m.height*2/3 - 3
	appHeight := m.height - traceHeight - 5

	if traceHeight < 3 {
		traceHeight = 3
	}
	if appHeight < 3 {
		appHeight = 3
	}

	m.traceView = viewport.New(contentWidth, traceHeight)
	m.traceView.SetContent(strings.Join(m.traces, "\n"))

	m.appView = viewport.New(contentWidth, appHeight)
	m.appView.SetContent(strings.Join(m.appLines, "\n"))

	return m
}

func (m Model) View() string {
	if !m.ready {
		return "Initializing..."
	}
	if m.quitting {
		return ""
	}

	traceTitle := titleStyle.Render("Traces")

	traceBox := borderStyle.
		Width(m.width - 2).
		Render(traceTitle + "\n" + m.traceView.View())

	help := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		PaddingLeft(2).
		Render("q: quit")

	if m.tracesOnly {
		return traceBox + "\n" + help
	}

	appTitle := titleStyle.Render("App Output")
	appBox := borderStyle.
		Width(m.width - 2).
		Render(appTitle + "\n" + m.appView.View())

	return traceBox + "\n" + appBox + "\n" + help
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

// AppWriter returns an io.Writer that sends each line to the TUI
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
