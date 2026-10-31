package model

import (
	"context"
	"database/sql"
	"time"
)

// recentCustomers lists customers who signed up after a given instant.
const recentCustomers = "SELECT customer_id, email FROM customers WHERE signed_up_at >= $1"

// RecentCustomers returns the customers who signed up since since.
func RecentCustomers(ctx context.Context, db *sql.DB, since time.Time) ([]Customer, error) {
	rows, err := db.QueryContext(ctx, recentCustomers, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Customer
	for rows.Next() {
		var c Customer
		if err := rows.Scan(&c.CustomerID, &c.Email); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
