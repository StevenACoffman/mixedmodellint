package app

import (
	"context"
	"database/sql"
	"time"
)

const recordView = "INSERT INTO page_views (page_view_id, viewed_at) VALUES ($1, $2) " +
	"ON CONFLICT (page_view_id) DO NOTHING"

// RecordView stores the view time to the minute (§9.8-B).
func RecordView(ctx context.Context, db *sql.DB, id int64) error {
	_, err := db.ExecContext(ctx, recordView, id, time.Now().Truncate(time.Minute))
	return err
}
