package runtime_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"io"
	"sync"
	"testing"

	"github.com/mickamy/go-trace/runtime"
	"github.com/mickamy/go-trace/tracer"
)

func init() {
	sql.Register("fakedriver", &fakeDriver{})
}

func TestOpenDB(t *testing.T) {
	t.Parallel()

	rec := &recordingSender{}
	tr := runtime.NewTracer(rec)

	db, err := runtime.OpenDB(tr, "fakedriver", "")
	if err != nil {
		t.Fatalf("OpenDB() error: %v", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.PingContext(t.Context()); err != nil {
		t.Fatalf("PingContext() error: %v", err)
	}
}

func TestOpenDB_QueryContext_Traced(t *testing.T) {
	t.Parallel()

	rec := &recordingSender{}
	tr := runtime.NewTracer(rec)

	db, err := runtime.OpenDB(tr, "fakedriver", "")
	if err != nil {
		t.Fatalf("OpenDB() error: %v", err)
	}
	defer func() { _ = db.Close() }()

	rows, err := db.QueryContext(t.Context(), "SELECT id FROM users")
	if err != nil {
		t.Fatalf("QueryContext() error: %v", err)
	}
	defer func() { _ = rows.Close() }()
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err() = %v", err)
	}

	events := rec.Events()
	if len(events) < 2 {
		t.Fatalf("len(events) = %d, want >= 2", len(events))
	}

	var found bool
	for _, ev := range events {
		if ev.Type == tracer.EventSpanEnd && ev.Attrs["query"] == "SELECT id FROM users" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected span end event with query attribute")
	}
}

func TestOpenDB_ExecContext_Traced(t *testing.T) {
	t.Parallel()

	rec := &recordingSender{}
	tr := runtime.NewTracer(rec)

	db, err := runtime.OpenDB(tr, "fakedriver", "")
	if err != nil {
		t.Fatalf("OpenDB() error: %v", err)
	}
	defer func() { _ = db.Close() }()

	_, err = db.ExecContext(t.Context(), "INSERT INTO users (name) VALUES ('test')")
	if err != nil {
		t.Fatalf("ExecContext() error: %v", err)
	}

	events := rec.Events()
	var found bool
	for _, ev := range events {
		if ev.Type == tracer.EventSpanEnd && ev.Attrs["query"] == "INSERT INTO users (name) VALUES ('test')" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected span end event with query attribute")
	}
}

func TestOpenDB_PreparedQuery_Traced(t *testing.T) {
	t.Parallel()

	rec := &recordingSender{}
	tr := runtime.NewTracer(rec)

	db, err := runtime.OpenDB(tr, "fakedriver", "")
	if err != nil {
		t.Fatalf("OpenDB() error: %v", err)
	}
	defer func() { _ = db.Close() }()

	stmt, err := db.PrepareContext(t.Context(), "SELECT name FROM users WHERE id = ?")
	if err != nil {
		t.Fatalf("PrepareContext() error: %v", err)
	}
	defer func() { _ = stmt.Close() }()

	rows, err := stmt.QueryContext(t.Context(), 1)
	if err != nil {
		t.Fatalf("QueryContext() error: %v", err)
	}
	defer func() { _ = rows.Close() }()
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err() = %v", err)
	}

	events := rec.Events()
	var found bool
	for _, ev := range events {
		if ev.Type == tracer.EventSpanEnd && ev.Attrs["query"] == "SELECT name FROM users WHERE id = ?" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected span end event with prepared query attribute")
	}
}

func TestOpenDB_SpanKindIsSQL(t *testing.T) {
	t.Parallel()

	rec := &recordingSender{}
	tr := runtime.NewTracer(rec)

	db, err := runtime.OpenDB(tr, "fakedriver", "")
	if err != nil {
		t.Fatalf("OpenDB() error: %v", err)
	}
	defer func() { _ = db.Close() }()

	_, err = db.ExecContext(t.Context(), "DELETE FROM users")
	if err != nil {
		t.Fatalf("ExecContext() error: %v", err)
	}

	events := rec.Events()
	for _, ev := range events {
		if ev.Type == tracer.EventSpanStart {
			if ev.Kind != tracer.SpanKindSQL {
				t.Errorf("Kind = %v, want SpanKindSQL", ev.Kind)
			}
			break
		}
	}
}

// --- fake driver implementation ---

type fakeDriver struct{}

func (d *fakeDriver) Open(_ string) (driver.Conn, error) {
	return &fakeConn{}, nil
}

type fakeConn struct{}

func (c *fakeConn) Prepare(query string) (driver.Stmt, error) {
	return &fakeStmt{query: query}, nil
}

func (c *fakeConn) Close() error { return nil }

func (c *fakeConn) Begin() (driver.Tx, error) {
	return &fakeTx{}, nil
}

func (c *fakeConn) QueryContext(_ context.Context, _ string, _ []driver.NamedValue) (driver.Rows, error) {
	return &fakeRows{}, nil
}

func (c *fakeConn) ExecContext(_ context.Context, _ string, _ []driver.NamedValue) (driver.Result, error) {
	return fakeResult{}, nil
}

type fakeStmt struct {
	query string
}

func (s *fakeStmt) Close() error                                 { return nil }
func (s *fakeStmt) NumInput() int                                { return -1 }
func (s *fakeStmt) Exec(_ []driver.Value) (driver.Result, error) { return fakeResult{}, nil }
func (s *fakeStmt) Query(_ []driver.Value) (driver.Rows, error)  { return &fakeRows{}, nil }

type fakeResult struct{}

func (r fakeResult) LastInsertId() (int64, error) { return 0, nil }
func (r fakeResult) RowsAffected() (int64, error) { return 0, nil }

type fakeRows struct {
	mu     sync.Mutex
	closed bool
}

func (r *fakeRows) Columns() []string { return []string{"id"} }
func (r *fakeRows) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	return nil
}
func (r *fakeRows) Next(_ []driver.Value) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return io.EOF
	}
	r.closed = true
	return io.EOF
}

type fakeTx struct{}

func (t *fakeTx) Commit() error   { return nil }
func (t *fakeTx) Rollback() error { return nil }
