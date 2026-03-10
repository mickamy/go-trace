package runtime

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"sync/atomic"

	"github.com/mickamy/go-trace/tracer"
)

var driverSeq atomic.Int64

// OpenDB opens a database connection wrapped with tracing.
// It registers a tracing driver that records all queries as spans.
func OpenDB(t *Tracer, driverName, dsn string) (*sql.DB, error) {
	origDB, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("open original driver: %w", err)
	}
	origDriver := origDB.Driver()
	if err := origDB.Close(); err != nil {
		return nil, fmt.Errorf("close probe connection: %w", err)
	}

	wrappedName := fmt.Sprintf("gotrace:%s:%d", driverName, driverSeq.Add(1))
	sql.Register(wrappedName, &tracingDriver{
		driver: origDriver,
		tracer: t,
	})

	db, err := sql.Open(wrappedName, dsn)
	if err != nil {
		return nil, fmt.Errorf("open traced driver: %w", err)
	}
	return db, nil
}

// tracingDriver wraps a driver.Driver to add tracing.
type tracingDriver struct {
	driver driver.Driver
	tracer *Tracer
}

func (d *tracingDriver) Open(name string) (driver.Conn, error) {
	conn, err := d.driver.Open(name)
	if err != nil {
		return nil, err //nolint:wrapcheck // transparent wrapper
	}
	return &tracingConn{conn: conn, tracer: d.tracer}, nil
}

// tracingConn wraps a driver.Conn to trace queries.
type tracingConn struct {
	conn   driver.Conn
	tracer *Tracer
}

func (c *tracingConn) Prepare(query string) (driver.Stmt, error) {
	stmt, err := c.conn.Prepare(query)
	if err != nil {
		return nil, err //nolint:wrapcheck // transparent wrapper
	}
	return &tracingStmt{stmt: stmt, query: query, tracer: c.tracer}, nil
}

func (c *tracingConn) Close() error {
	return c.conn.Close() //nolint:wrapcheck // transparent wrapper
}

func (c *tracingConn) Begin() (driver.Tx, error) {
	return c.conn.Begin() //nolint:wrapcheck,staticcheck // transparent wrapper; Begin required by driver.Conn
}

// QueryContext implements driver.QueryerContext.
func (c *tracingConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	qc, ok := c.conn.(driver.QueryerContext)
	if !ok {
		return nil, driver.ErrSkip
	}

	ctx, finish := c.tracer.Enter(ctx, "SQL Query", tracer.SpanKindSQL)
	rows, err := qc.QueryContext(ctx, query, args)

	attrs := map[string]string{"query": query}
	if err != nil {
		attrs["error"] = err.Error()
	}
	finish(attrs)

	return rows, err //nolint:wrapcheck // transparent wrapper
}

// ExecContext implements driver.ExecerContext.
func (c *tracingConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	ec, ok := c.conn.(driver.ExecerContext)
	if !ok {
		return nil, driver.ErrSkip
	}

	ctx, finish := c.tracer.Enter(ctx, "SQL Exec", tracer.SpanKindSQL)
	result, err := ec.ExecContext(ctx, query, args)

	attrs := map[string]string{"query": query}
	if err != nil {
		attrs["error"] = err.Error()
	}
	finish(attrs)

	return result, err //nolint:wrapcheck // transparent wrapper
}

// tracingStmt wraps a driver.Stmt to trace prepared statement execution.
type tracingStmt struct {
	stmt   driver.Stmt
	query  string
	tracer *Tracer
}

func (s *tracingStmt) Close() error {
	return s.stmt.Close() //nolint:wrapcheck // transparent wrapper
}

func (s *tracingStmt) NumInput() int {
	return s.stmt.NumInput()
}

func (s *tracingStmt) Exec(args []driver.Value) (driver.Result, error) {
	return s.stmt.Exec(args) //nolint:wrapcheck,staticcheck // transparent wrapper; Exec required by driver.Stmt
}

func (s *tracingStmt) Query(args []driver.Value) (driver.Rows, error) {
	return s.stmt.Query(args) //nolint:wrapcheck,staticcheck // transparent wrapper; Query required by driver.Stmt
}

// QueryContext implements driver.StmtQueryContext.
func (s *tracingStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	ctx, finish := s.tracer.Enter(ctx, "SQL Query", tracer.SpanKindSQL)

	var rows driver.Rows
	var err error
	if sc, ok := s.stmt.(driver.StmtQueryContext); ok {
		rows, err = sc.QueryContext(ctx, args)
	} else {
		rows, err = s.stmt.Query(namedToValues(args)) //nolint:staticcheck // fallback for legacy drivers
	}

	attrs := map[string]string{"query": s.query}
	if err != nil {
		attrs["error"] = err.Error()
	}
	finish(attrs)

	return rows, err //nolint:wrapcheck // transparent wrapper
}

// ExecContext implements driver.StmtExecContext.
func (s *tracingStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	ctx, finish := s.tracer.Enter(ctx, "SQL Exec", tracer.SpanKindSQL)

	var result driver.Result
	var err error
	if sc, ok := s.stmt.(driver.StmtExecContext); ok {
		result, err = sc.ExecContext(ctx, args)
	} else {
		result, err = s.stmt.Exec(namedToValues(args)) //nolint:staticcheck // fallback for legacy drivers
	}

	attrs := map[string]string{"query": s.query}
	if err != nil {
		attrs["error"] = err.Error()
	}
	finish(attrs)

	return result, err //nolint:wrapcheck // transparent wrapper
}

func namedToValues(named []driver.NamedValue) []driver.Value {
	values := make([]driver.Value, len(named))
	for i, nv := range named {
		values[i] = nv.Value
	}
	return values
}
