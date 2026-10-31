package gopackages

import (
	"reflect"
	"strings"
	"unicode"

	mml "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// persistenceTags are the struct tag keys that mark a field as stored or
// serialized, in the order their names win: a db tag names the column even
// when a json tag also exists.
func persistenceTags() []string {
	return []string{"db", "gorm", "bun", "sql", "bson", "parquet", "avro", "json"}
}

// tagName returns the persisted name a field's tags give it, whether a
// storage tag (anything but json) is present, and whether a tag says the
// value may be omitted. A "-" name means the field is not persisted.
//
// A json tag names the field but does not make the struct an entity: a
// struct that is only serialized is an API payload or a report, not stored
// data, and asking it for a grain would be noise.
func tagName(tag reflect.StructTag) (name string, stored, omitempty bool) {
	for _, key := range persistenceTags() {
		v, ok := tag.Lookup(key)
		if !ok {
			continue
		}
		stored = stored || key != "json"
		n, opts := tagValue(key, v)
		omitempty = omitempty || strings.Contains(opts, "omitempty")
		if name == "" {
			name = n
		}
	}
	return name, stored, omitempty
}

// tagValue splits one tag's value into the column name and its options. gorm
// writes `gorm:"column:x;primaryKey"`; the others write `json:"x,omitempty"`.
func tagValue(key, v string) (name, opts string) {
	if key == "gorm" {
		for part := range strings.SplitSeq(v, ";") {
			if col, ok := strings.CutPrefix(part, "column:"); ok {
				name = col
			}
		}
		return name, v
	}
	name, opts, _ = strings.Cut(v, ",")
	return name, opts
}

// snake converts a Go identifier to snake_case without singularizing, the
// way sqlx and most mappers name an untagged field's column: OrderLines is
// order_lines and CustomerID is customer_id.
func snake(name string) string {
	runes := []rune(name)
	var b strings.Builder
	for i, r := range runes {
		if unicode.IsUpper(r) && i > 0 {
			prev := runes[i-1]
			nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if unicode.IsLower(prev) || unicode.IsDigit(prev) ||
				unicode.IsUpper(prev) && nextLower {
				b.WriteByte('_')
			}
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// logicalType maps a Go type to the IR's logical type and whether the Go
// type itself admits a missing value (a pointer or a sql.Null* wrapper).
func logicalType(raw string) (mml.LogicalType, bool) {
	nullable := strings.HasPrefix(raw, "*")
	base := strings.TrimPrefix(raw, "*")
	if strings.HasPrefix(base, "sql.Null") || strings.HasPrefix(base, "null.") {
		nullable = true
	}
	switch {
	case base == "[]byte" || base == "json.RawMessage":
		if base == "json.RawMessage" {
			return mml.TypeJSON, nullable
		}
		return mml.TypeBytes, nullable
	case strings.HasPrefix(base, "[]") || strings.HasPrefix(base, "["):
		return mml.TypeArray, nullable
	case strings.HasPrefix(base, "map["):
		return mml.TypeJSON, nullable
	}
	return scalarType(base), nullable
}

func scalarType(base string) mml.LogicalType {
	types := map[string]mml.LogicalType{
		"string": mml.TypeString, "sql.NullString": mml.TypeString, "null.String": mml.TypeString,
		"bool": mml.TypeBool, "sql.NullBool": mml.TypeBool,
		"float32": mml.TypeFloat, "float64": mml.TypeFloat, "sql.NullFloat64": mml.TypeFloat,
		"time.Time": mml.TypeTimestamptz, "sql.NullTime": mml.TypeTimestamptz,
		"null.Time": mml.TypeTimestamptz, "civil.Date": mml.TypeDate,
		"civil.DateTime": mml.TypeTimestampNaive, "civil.Time": mml.TypeTime,
		"time.Duration":   mml.TypeInterval,
		"decimal.Decimal": mml.TypeDecimal, "decimal.NullDecimal": mml.TypeDecimal,
		"uuid.UUID": mml.TypeUUID, "uuid.NullUUID": mml.TypeUUID,
	}
	if t, ok := types[base]; ok {
		return t
	}
	for _, p := range []string{"int", "uint", "sql.NullInt", "null.Int"} {
		if strings.HasPrefix(base, p) {
			return mml.TypeInt
		}
	}
	return mml.TypeUnknown
}
