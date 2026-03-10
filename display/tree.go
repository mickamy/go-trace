package display

import (
	"fmt"
	"io"
	"sort"
	"sync"

	"github.com/mickamy/go-trace/tracer"
)

// Renderer buffers completed spans and prints a trace tree
// when the root span (no parent) arrives.
type Renderer struct {
	w      io.Writer
	mu     sync.Mutex
	traces map[string][]tracer.Span
}

// NewRenderer creates a renderer that writes trees to w.
func NewRenderer(w io.Writer) *Renderer {
	return &Renderer{
		w:      w,
		traces: make(map[string][]tracer.Span),
	}
}

// Add buffers a completed span. When the root span arrives,
// the full trace tree is printed and the buffer is cleared.
func (r *Renderer) Add(traceID string, span tracer.Span) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.traces[traceID] = append(r.traces[traceID], span)

	if span.ParentID == "" {
		printTrace(r.w, r.traces[traceID])
		delete(r.traces, traceID)
	}
}

// printTrace builds a tree from flat spans and prints it.
func printTrace(w io.Writer, spans []tracer.Span) {
	root := buildTree(spans)
	printNode(w, root, "", true, true)
	fmt.Fprintln(w)
}

// buildTree assembles a span tree from a flat slice.
func buildTree(spans []tracer.Span) tracer.Span {
	var root tracer.Span
	children := make(map[string][]tracer.Span)

	for _, s := range spans {
		if s.ParentID == "" {
			root = s
		} else {
			children[s.ParentID] = append(children[s.ParentID], s)
		}
	}

	return attachChildren(root, children)
}

func attachChildren(span tracer.Span, childMap map[string][]tracer.Span) tracer.Span {
	kids := childMap[span.ID]
	sort.Slice(kids, func(i, j int) bool {
		return kids[i].StartTime.Before(kids[j].StartTime)
	})
	for _, child := range kids {
		child = attachChildren(child, childMap)
		span = span.WithChild(child)
	}
	return span
}

func printNode(w io.Writer, span tracer.Span, prefix string, isLast, isRoot bool) {
	if isRoot {
		fmt.Fprintf(w, "%s [%s] %v\n", span.Name, span.Kind, span.Duration())
	} else {
		connector := "├── "
		if isLast {
			connector = "└── "
		}
		fmt.Fprintf(w, "%s%s%s [%s] %v\n", prefix, connector, span.Name, span.Kind, span.Duration())
	}

	childPrefix := prefix
	if !isRoot {
		if isLast {
			childPrefix = prefix + "    "
		} else {
			childPrefix = prefix + "│   "
		}
	}

	for i, child := range span.Children {
		printNode(w, child, childPrefix, i == len(span.Children)-1, false)
	}
}
