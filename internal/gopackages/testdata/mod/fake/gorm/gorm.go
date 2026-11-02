// Package gorm stands in for gorm.io/gorm: the tracer matches calls by
// package path, type and method, so only the signatures matter.
package gorm

// DB is gorm's database handle.
type DB struct{}

// Create inserts value's stored fields.
func (db *DB) Create(value any) *DB { return db }

// Save upserts value's stored fields on its primary key.
func (db *DB) Save(value any) *DB { return db }
