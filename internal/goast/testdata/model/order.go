package model

import "time"

// Audit is embedded in every persisted struct.
type Audit struct {
	CreatedAt time.Time  `db:"created_at"`
	UpdatedAt *time.Time `db:"updated_at"`
}

// Order is one placed order.
//
//mml:grain one row per order
//mml:layer oltp
type Order struct {
	ID         int64       `db:"id"`
	CustomerID int64       `db:"customer_id"`
	Status     string      `db:"status" mml:"null=forbidden"`
	Lines      []OrderLine `db:"-"`
	Audit
	internal string
}

// OrderLine is a line of an order.
type OrderLine struct {
	OrderID int64     `db:"order_id" mml:"pk"`
	LineNo  int       `db:"line_no" mml:"pk"`
	Phones  [3]string `json:"phones"`
	Note    string    `json:"note,omitempty"`
}

// Customer has no tags and no directives, so it is not an entity.
type Customer struct {
	Name string
}

// Config is not persisted.
type Config struct{ Debug bool }

// Response is serialized but not stored, so it is not an entity.
type Response struct {
	OK bool `json:"ok"`
}
