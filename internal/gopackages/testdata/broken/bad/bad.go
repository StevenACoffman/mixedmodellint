// Package bad does not type-check.
package bad

// Thing is persisted, but its file has a type error below.
type Thing struct {
	ID int64 `db:"id"`
}

var broken int = "not an int"
