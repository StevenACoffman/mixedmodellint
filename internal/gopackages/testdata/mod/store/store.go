// Package store runs SQL against the model.
package store

import (
	"context"
	"database/sql"
	"fmt"
)

const ordersTable = "orders"

// listOrders is a constant built by concatenation.
const listOrders = "SELECT id, status FROM " + ordersTable

// DBTX is the interface sqlc generates.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Cache is not a database; its Get takes a key, not SQL.
type Cache struct{}

// Get returns a cached value.
func (Cache) Get(key string) string { return key }

// Store reads and writes orders.
type Store struct {
	db    *sql.DB
	q     DBTX
	cache Cache
}

// List lists orders.
func (s *Store) List(ctx context.Context) error {
	_, err := s.db.QueryContext(ctx, listOrders)
	return err
}

// ByStatus filters by status in one of two ways.
func (s *Store) ByStatus(ctx context.Context, open bool) error {
	q := "SELECT id FROM orders WHERE status = 'open'"
	if !open {
		q = "SELECT id FROM orders WHERE status <> 'open'"
	}
	_, err := s.db.QueryContext(ctx, q+" ORDER BY id")
	return err
}

// Touch updates through the sqlc interface.
func (s *Store) Touch(ctx context.Context) error {
	_, err := s.q.ExecContext(ctx, "UPDATE orders SET updated_at = now()")
	return err
}

// Dynamic builds SQL at run time, which cannot be folded.
func (s *Store) Dynamic(ctx context.Context, table string) error {
	_, err := s.db.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s", table))
	return err
}

// Cached is not SQL.
func (s *Store) Cached() string { return s.cache.Get("SELECT 1") }
