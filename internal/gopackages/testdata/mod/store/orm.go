package store

import (
	"context"
	"time"

	"github.com/jmoiron/sqlx"
	"gorm.io/gorm"

	"example.com/mod/model"
)

// LineParams binds sqlx's :name parameters by snake-cased field name.
type LineParams struct {
	OrderID  int64
	OrderFee int64
}

// AddNamed copies the order's fee onto a line through sqlx named parameters.
func AddNamed(ctx context.Context, db *sqlx.DB, o *model.Order) error {
	_, err := db.NamedExecContext(ctx,
		"INSERT INTO order_lines (order_id, order_fee) VALUES (:order_id, :order_fee::numeric)",
		LineParams{
			OrderID:  o.ID,
			OrderFee: o.ShippingFee,
		})
	return err
}

// exec wraps ExecContext, as many stores do.
func (s *Store) exec(ctx context.Context, q string, args ...any) error {
	_, err := s.db.ExecContext(ctx, q, args...)
	return err
}

// Stamp writes an hour-truncated time through the wrapper.
func (s *Store) Stamp(ctx context.Context, o *model.Order) error {
	return s.exec(ctx, "UPDATE clicks SET seen_at = $1", o.PlacedAt.Truncate(time.Hour))
}

// SaveLine stores a line with gorm.
func SaveLine(db *gorm.DB, o *model.Order) {
	db.Create(&model.OrderLine{
		OrderID: o.ID,
		Note:    "gift",
	})
}
