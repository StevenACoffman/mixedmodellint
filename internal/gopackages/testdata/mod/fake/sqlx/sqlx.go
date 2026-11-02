// Package sqlx stands in for github.com/jmoiron/sqlx: the tracer matches
// calls by package path, type and method, so only the signatures matter.
package sqlx

import (
	"context"
	"database/sql"
)

// DB is sqlx's database handle.
type DB struct{}

// NamedExecContext binds :name parameters from arg's fields.
func (db *DB) NamedExecContext(ctx context.Context, query string, arg any) (sql.Result, error) {
	return nil, nil
}
