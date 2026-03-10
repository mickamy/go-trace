package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

var db *sql.DB

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:5432/example?sslmode=disable"
	}

	var err error
	db, err = sql.Open("pgx", dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "db open:", err)
		os.Exit(1)
	}
	defer db.Close()

	// Wait for database to be ready.
	for range 30 {
		if err := db.Ping(); err == nil {
			break
		}
		time.Sleep(time.Second)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /hello/{name}", handleHello)
	mux.HandleFunc("GET /slow", handleSlow)
	mux.HandleFunc("GET /users", handleUsers)

	addr := ":8080"
	if port := os.Getenv("PORT"); port != "" {
		addr = ":" + port
	}

	fmt.Println("listening on", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		fmt.Fprintln(os.Stderr, "server error:", err)
		os.Exit(1)
	}
}

func handleHello(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	msg := Greet(r.Context(), name)
	fmt.Fprint(w, msg)
}

func handleSlow(w http.ResponseWriter, r *http.Request) {
	result := HeavyWork(r.Context())
	fmt.Fprint(w, result)
}

func handleUsers(w http.ResponseWriter, r *http.Request) {
	users, err := ListUsers(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(users)
}

// Greet returns a greeting message.
func Greet(_ context.Context, name string) string {
	return fmt.Sprintf("Hello, %s!", name)
}

// HeavyWork simulates a slow operation.
func HeavyWork(ctx context.Context) string {
	_ = ctx
	time.Sleep(500 * time.Millisecond)
	return "done"
}

// User represents a row in the users table.
type User struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// ListUsers queries all users from the database.
func ListUsers(ctx context.Context) ([]User, error) {
	rows, err := db.QueryContext(ctx, "SELECT id, name FROM users ORDER BY id")
	if err != nil {
		return nil, fmt.Errorf("query users: %w", err)
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Name); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, u)
	}
	return users, rows.Err()
}
