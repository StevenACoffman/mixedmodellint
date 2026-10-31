package store

import (
	"context"
	"time"

	"example.com/mod/model"
)

// CreateLineParams is the argument struct sqlc generates for CreateLine.
type CreateLineParams struct {
	OrderID  int64
	OrderFee int64
}

const createLine = `-- name: CreateLine :exec
INSERT INTO order_lines (order_id, order_fee) VALUES ($1, $2)
`

// Queries is what sqlc generates around DBTX.
type Queries struct{ db DBTX }

// CreateLine is the method sqlc generates.
func (q *Queries) CreateLine(ctx context.Context, arg CreateLineParams) error {
	_, err := q.db.ExecContext(ctx, createLine, arg.OrderID, arg.OrderFee)
	return err
}

// AddLine copies the order's fee onto a line through sqlc.
func AddLine(ctx context.Context, q *Queries, o *model.Order) error {
	return q.CreateLine(ctx, CreateLineParams{
		OrderID:  o.ID,
		OrderFee: o.ShippingFee,
	})
}

// RecordClick truncates times on the way to the database.
func (s *Store) RecordClick(ctx context.Context, o *model.Order) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO clicks (clicked_at, placed_at, placed_on) VALUES ($1, $2, $3)",
		time.Now().Truncate(time.Second),
		o.PlacedAt.UTC(),
		o.PlacedAt.Format(time.DateOnly),
	)
	return err
}

// RoundTrip drops precision by rebuilding times from whole units.
func (s *Store) RoundTrip(ctx context.Context, t time.Time, precise bool) error {
	v := time.Unix(t.Unix(), 0)
	if precise {
		v = time.UnixMilli(t.UnixMilli())
	}
	_, err := s.db.ExecContext(ctx, "UPDATE clicks SET seen_at = $1", v)
	return err
}
