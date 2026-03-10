package tracer

import (
	"maps"
	"time"
)

// SpanKind represents the type of operation a span tracks.
type SpanKind int

const (
	SpanKindFunction SpanKind = iota
	SpanKindHTTP
	SpanKindSQL
)

func (k SpanKind) String() string {
	switch k {
	case SpanKindFunction:
		return "function"
	case SpanKindHTTP:
		return "http"
	case SpanKindSQL:
		return "sql"
	default:
		return "unknown"
	}
}

// Span represents a single unit of work in a trace.
// Span is a value type; all mutation methods return a new copy.
type Span struct {
	ID        string            `json:"id"`
	TraceID   string            `json:"trace_id"`
	ParentID  string            `json:"parent_id,omitzero"`
	Name      string            `json:"name"`
	Kind      SpanKind          `json:"kind"`
	StartTime time.Time         `json:"start_time"`
	EndTime   time.Time         `json:"end_time"`
	Attrs     map[string]string `json:"attrs,omitzero"`
	Children  []Span            `json:"children,omitzero"`
}

// NewSpan creates a span with the required fields.
func NewSpan(id, traceID, name string, kind SpanKind, startTime, endTime time.Time) Span {
	return Span{
		ID:        id,
		TraceID:   traceID,
		Name:      name,
		Kind:      kind,
		StartTime: startTime,
		EndTime:   endTime,
	}
}

// Duration returns the elapsed time of the span.
func (s Span) Duration() time.Duration {
	return s.EndTime.Sub(s.StartTime)
}

// WithParentID returns a new Span with the given parent ID.
func (s Span) WithParentID(parentID string) Span {
	s.ParentID = parentID
	return s
}

// WithAttr returns a new Span with the key-value pair added.
func (s Span) WithAttr(key, value string) Span {
	newAttrs := make(map[string]string, len(s.Attrs)+1)
	maps.Copy(newAttrs, s.Attrs)
	newAttrs[key] = value
	s.Attrs = newAttrs
	return s
}

// Clone returns a deep copy of the span, including Attrs and Children.
func (s Span) Clone() Span {
	if s.Attrs != nil {
		s.Attrs = maps.Clone(s.Attrs)
	}
	if s.Children != nil {
		children := make([]Span, len(s.Children))
		for i, child := range s.Children {
			children[i] = child.Clone()
		}
		s.Children = children
	}
	return s
}

// WithChild returns a new Span with the child appended.
func (s Span) WithChild(child Span) Span {
	newChildren := make([]Span, len(s.Children)+1)
	copy(newChildren, s.Children)
	newChildren[len(s.Children)] = child
	s.Children = newChildren
	return s
}
