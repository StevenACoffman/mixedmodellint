// Package audit holds a mixin embedded by persisted structs in other packages.
package audit

import "time"

// Stamps is embedded in every persisted struct.
type Stamps struct {
	CreatedAt time.Time  `db:"created_at"`
	UpdatedAt *time.Time `db:"updated_at"`
}
