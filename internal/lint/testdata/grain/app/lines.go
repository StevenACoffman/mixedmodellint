package app

import (
	"context"
	"database/sql"
)

// Order is the Go side of orders.
//
//mml:grain one row per order
type Order struct {
	OrderID     int64   `db:"order_id" mml:"pk"`
	ShippingFee float64 `db:"shipping_fee"`
}

// DBTX is the interface sqlc generates.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Queries is what sqlc generates around DBTX.
type Queries struct{ db DBTX }

// CreateLineParams is the argument struct sqlc generates.
type CreateLineParams struct {
	OrderID  int64
	LineNo   int32
	OrderFee float64
}

const createLine = `-- name: CreateLine :exec
INSERT INTO order_lines (order_id, line_no, order_fee)
VALUES ($1, $2, $3)
`

// CreateLine is the method sqlc generates from queries/lines.sql.
func (q *Queries) CreateLine(ctx context.Context, arg CreateLineParams) error {
	_, err := q.db.ExecContext(ctx, createLine, arg.OrderID, arg.LineNo, arg.OrderFee)
	return err
}

// AddLine copies the order's shipping fee onto every line (§7.4-D).
func AddLine(ctx context.Context, q *Queries, o Order, n int32) error {
	return q.CreateLine(ctx, CreateLineParams{
		OrderID:  o.OrderID,
		LineNo:   n,
		OrderFee: o.ShippingFee,
	})
}
