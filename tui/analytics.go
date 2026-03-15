package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/mickamy/go-trace/analysis"
)

// analyticsTab represents a tab in the analytics view.
type analyticsTab int

const (
	tabEndpoint analyticsTab = iota
	tabSQL
	tabFunction
	tabN1
)

const analyticsTabCount = 4

func (t analyticsTab) String() string {
	switch t {
	case tabEndpoint:
		return "Endpoint"
	case tabSQL:
		return "SQL"
	case tabFunction:
		return "Function"
	case tabN1:
		return "N+1"
	default:
		return "Endpoint"
	}
}

func (m Model) renderAnalyticsPane() string {
	innerWidth := max(m.width-4, 20)
	visibleRows := m.analyticsVisibleRows()

	lines := m.analyticsDisplayLines(innerWidth)
	start := min(m.analyticsScroll, len(lines))
	end := min(start+visibleRows, len(lines))
	visible := lines[start:end]

	for len(visible) < visibleRows {
		visible = append(visible, "")
	}

	content := strings.Join(visible, "\n")
	title := fmt.Sprintf(" Analytics (%d traces) ", m.report.TraceCount)
	return m.renderBox(innerWidth, content, title)
}

func (m Model) analyticsDisplayLines(innerWidth int) []string {
	if m.report.TraceCount == 0 {
		return []string{"", "  No traces collected"}
	}

	// Tab bar
	tabBar := m.renderTabBar()
	lines := []string{tabBar, ""}

	sortLabel := "sort: " + m.analyticsSort.String()
	faint := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

	switch m.analyticsTab {
	case tabEndpoint:
		lines = append(lines, m.renderEndpointTable(innerWidth, faint, sortLabel)...)
	case tabSQL:
		lines = append(lines, m.renderSQLTable(innerWidth, faint, sortLabel)...)
	case tabFunction:
		lines = append(lines, m.renderFunctionTable(innerWidth, faint, sortLabel)...)
	case tabN1:
		lines = append(lines, m.renderN1Table(innerWidth, faint)...)
	}

	return lines
}

func (m Model) renderTabBar() string {
	active := lipgloss.NewStyle().Bold(true).Underline(true)
	inactive := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

	tabs := []analyticsTab{tabEndpoint, tabSQL, tabFunction, tabN1}
	var parts []string
	for _, tab := range tabs {
		label := fmt.Sprintf("[%s]", tab.String())
		if tab == m.analyticsTab {
			parts = append(parts, active.Render(label))
		} else {
			parts = append(parts, inactive.Render(label))
		}
	}
	return "  " + strings.Join(parts, "  ")
}

func (m Model) renderEndpointTable(innerWidth int, faint lipgloss.Style, sortLabel string) []string {
	stats := m.sortedEndpoints()
	if len(stats) == 0 {
		return []string{"  No HTTP endpoints found"}
	}

	colMethod := 7
	colCount := 6
	colTotal := 10
	colAvg := 10
	colP95 := 10
	colMax := 10
	fixedCols := colMethod + colCount + colTotal + colAvg + colP95 + colMax + 6 // 6 = gaps
	colPath := max(innerWidth-fixedCols-2, 10)                                  // 2 = left margin

	header := fmt.Sprintf("  %-*s %-*s %*s %*s %*s %*s %*s",
		colMethod, "Method",
		colPath, "Path",
		colCount, "Count",
		colTotal, "Total",
		colAvg, "Avg",
		colP95, "P95",
		colMax, "Max",
	)

	lines := []string{
		faint.Render(fmt.Sprintf("  %*s", innerWidth-2, sortLabel)),
		lipgloss.NewStyle().Bold(true).Render(header),
	}

	for i, ep := range stats {
		cursor := "  "
		if i == m.analyticsCursor {
			cursor = "▶ "
		}
		path := truncate(ep.Path, colPath)
		line := fmt.Sprintf("%s%-*s %-*s %*d %*s %*s %*s %*s",
			cursor,
			colMethod, ep.Method,
			colPath, path,
			colCount, ep.Count,
			colTotal, formatDuration(ep.Total),
			colAvg, formatDuration(ep.Avg),
			colP95, formatDuration(ep.P95),
			colMax, formatDuration(ep.Max),
		)
		lines = append(lines, line)
	}
	return lines
}

func (m Model) renderSQLTable(innerWidth int, faint lipgloss.Style, sortLabel string) []string {
	stats := m.sortedSQL()
	if len(stats) == 0 {
		return []string{"  No SQL queries found"}
	}

	colCount := 6
	colTotal := 10
	colAvg := 10
	colP95 := 10
	colMax := 10
	fixedCols := colCount + colTotal + colAvg + colP95 + colMax + 5
	colQuery := max(innerWidth-fixedCols-2, 10)

	header := fmt.Sprintf("  %-*s %*s %*s %*s %*s %*s",
		colQuery, "Query",
		colCount, "Count",
		colTotal, "Total",
		colAvg, "Avg",
		colP95, "P95",
		colMax, "Max",
	)

	lines := []string{
		faint.Render(fmt.Sprintf("  %*s", innerWidth-2, sortLabel)),
		lipgloss.NewStyle().Bold(true).Render(header),
	}

	for i, sq := range stats {
		cursor := "  "
		if i == m.analyticsCursor {
			cursor = "▶ "
		}
		query := truncate(sq.Query, colQuery)
		line := fmt.Sprintf("%s%-*s %*d %*s %*s %*s %*s",
			cursor,
			colQuery, query,
			colCount, sq.Count,
			colTotal, formatDuration(sq.Total),
			colAvg, formatDuration(sq.Avg),
			colP95, formatDuration(sq.P95),
			colMax, formatDuration(sq.Max),
		)
		lines = append(lines, line)
	}
	return lines
}

func (m Model) renderFunctionTable(innerWidth int, faint lipgloss.Style, sortLabel string) []string {
	stats := m.sortedFunctions()
	if len(stats) == 0 {
		return []string{"  No functions found"}
	}

	colCount := 6
	colTotal := 10
	colAvg := 10
	colP95 := 10
	colMax := 10
	fixedCols := colCount + colTotal + colAvg + colP95 + colMax + 5
	colName := max(innerWidth-fixedCols-2, 10)

	header := fmt.Sprintf("  %-*s %*s %*s %*s %*s %*s",
		colName, "Name",
		colCount, "Count",
		colTotal, "Total",
		colAvg, "Avg",
		colP95, "P95",
		colMax, "Max",
	)

	lines := []string{
		faint.Render(fmt.Sprintf("  %*s", innerWidth-2, sortLabel)),
		lipgloss.NewStyle().Bold(true).Render(header),
	}

	for i, fn := range stats {
		cursor := "  "
		if i == m.analyticsCursor {
			cursor = "▶ "
		}
		name := truncate(fn.Name, colName)
		line := fmt.Sprintf("%s%-*s %*d %*s %*s %*s %*s",
			cursor,
			colName, name,
			colCount, fn.Count,
			colTotal, formatDuration(fn.Total),
			colAvg, formatDuration(fn.Avg),
			colP95, formatDuration(fn.P95),
			colMax, formatDuration(fn.Max),
		)
		lines = append(lines, line)
	}
	return lines
}

func (m Model) renderN1Table(innerWidth int, faint lipgloss.Style) []string {
	if len(m.report.N1) == 0 {
		return []string{"  No N+1 queries detected"}
	}

	colAvg := 8
	colMax := 8
	fixedCols := colAvg + colMax + 2
	remaining := max(innerWidth-fixedCols-2, 20)
	colEndpoint := max(remaining/3, 10)
	colQuery := max(remaining-colEndpoint-1, 10)

	header := fmt.Sprintf("  %-*s %-*s %*s %*s",
		colEndpoint, "Endpoint",
		colQuery, "Query",
		colAvg, "AvgCnt",
		colMax, "MaxCnt",
	)

	lines := []string{
		faint.Render("  N+1 query patterns (same query repeated 5+ times in a single trace)"),
		lipgloss.NewStyle().Bold(true).Render(header),
	}

	for i, n := range m.report.N1 {
		cursor := "  "
		if i == m.analyticsCursor {
			cursor = "▶ "
		}
		ep := truncate(n.Endpoint, colEndpoint)
		query := truncate(n.Query, colQuery)
		line := fmt.Sprintf("%s%-*s %-*s %*.1f %*d",
			cursor,
			colEndpoint, ep,
			colQuery, query,
			colAvg, n.AvgCount,
			colMax, n.MaxCount,
		)
		lines = append(lines, line)
	}
	return lines
}

func (m Model) renderAnalyticsFooter() string {
	faint := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	items := []string{
		"esc: traces",
		"tab: section",
		"s: sort",
		"j/k: scroll",
		"g/G: top/bottom",
		"q: quit",
	}
	return faint.Render("  " + strings.Join(items, "  "))
}

func (m Model) analyticsVisibleRows() int {
	return max(m.height-4, 3)
}

func (m Model) maxAnalyticsScroll() int {
	innerWidth := max(m.width-4, 20)
	lines := m.analyticsDisplayLines(innerWidth)
	return max(len(lines)-m.analyticsVisibleRows(), 0)
}

func (m Model) analyticsItemCount() int {
	switch m.analyticsTab {
	case tabEndpoint:
		return len(m.report.Endpoints)
	case tabSQL:
		return len(m.report.SQL)
	case tabFunction:
		return len(m.report.Functions)
	case tabN1:
		return len(m.report.N1)
	default:
		return 0
	}
}

func (m Model) sortedEndpoints() []analysis.EndpointStat {
	return m.cachedEndpoints
}

func (m Model) sortedSQL() []analysis.SQLStat {
	return m.cachedSQL
}

func (m Model) sortedFunctions() []analysis.FuncStat {
	return m.cachedFunctions
}
