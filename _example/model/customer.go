// Package model holds the persisted structs of the example.
package model

import "time"

// Customer mirrors the customers table; its grain is declared there.
type Customer struct {
	CustomerID int64     `db:"customer_id"`
	Email      string    `db:"email"`
	SignedUpAt time.Time `db:"signed_up_at"`
}
