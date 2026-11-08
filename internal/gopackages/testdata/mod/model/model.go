// Package model holds persisted structs.
package model

import (
	"time"

	"example.com/mod/audit"
)

// Status is a named string type.
type Status string

// Order is one placed order.
//
//mml:grain one row per order
type Order struct {
	ID         int64       `db:"id"`
	CustomerID int64       `db:"customer_id"`
	Status     Status      `db:"status"`
	Lines      []OrderLine `db:"-"`
	// ShippingFee is what the customer pays for delivery, in cents.
	ShippingFee int64     `db:"shipping_fee"`
	PlacedAt    time.Time `db:"placed_at"`
	audit.Stamps
}

// OrderLine is a line of an order.
type OrderLine struct {
	OrderID int64  `db:"order_id" mml:"pk"`
	LineNo  int    `db:"line_no" mml:"pk"`
	Order   *Order `db:"-"`
	Note    string `json:"note,omitempty" db:"note"` // what the customer asked for
}

// Response is serialized but not stored.
type Response struct {
	OK bool `json:"ok"`
}
