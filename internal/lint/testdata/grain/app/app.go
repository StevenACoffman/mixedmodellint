// Package app queries the grain fixture from Go.
package app

import (
	"context"
	"database/sql"
)

const bonusByCustomer = "SELECT c.customer_id, SUM(c.signup_bonus) " +
	"FROM customers c JOIN orders o ON o.customer_id = c.customer_id " +
	"GROUP BY c.customer_id"

// Bonuses double-counts each customer's bonus once per order (§7.2).
func Bonuses(ctx context.Context, db *sql.DB) (*sql.Rows, error) {
	return db.QueryContext(ctx, bonusByCustomer)
}
