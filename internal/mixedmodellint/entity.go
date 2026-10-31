// Package mixedmodellint is the pure core of the linter: the intermediate
// representation (IR) that every source kind is loaded into, the annotation
// vocabulary, cross-source linking, the rule designs, waivers, the baseline
// and the renderers.
//
// The package never touches the filesystem, the clock or a parser. Adapters
// (internal/postgres, internal/gopackages, internal/jsonschema, internal/sidecar)
// turn bytes into the values defined here, and internal/lint composes them.
// The design rationale for every check lives in rule_plan.md, and each design
// in this package carries the ID it has there, such as "7.1-B".
package mixedmodellint

import "strings"

// Source kinds.
const (
	SourceSQL SourceKind = iota + 1
	SourceGo
	SourceJSONSchema
)

// Entity kinds.
const (
	KindTable EntityKind = iota + 1
	KindView
	KindStruct
	KindSchema
)

// Logical types.
const (
	TypeUnknown LogicalType = iota
	TypeString
	TypeInt
	TypeDecimal
	TypeFloat
	TypeBool
	TypeDate
	TypeTimestamptz
	TypeTimestampNaive
	TypeTime
	TypeInterval
	TypeUUID
	TypeBytes
	TypeJSON
	TypeArray
	TypeObject
)

// SourceKind is the kind of source an entity was loaded from.
type SourceKind int

// EntityKind distinguishes stored entities from derived ones. Views are kept
// apart from tables because a view's grain is set by its query, not its keys.
type EntityKind int

// LogicalType is a field's type after normalization across SQL, Go and JSON
// Schema. TypeTimestampNaive is a timestamp without a zone (SQL `timestamp`,
// a JSON `date-time` string is always zoned), the type §9.2 reports.
type LogicalType int

// Position is a location in a source file. Line and Col are 1-based; zero
// means unknown, which SARIF renders as a file-level location.
type Position struct {
	File string
	Line int
	Col  int
}

// Field is one column, struct field or schema property.
//
// Nullable follows each source's own rule: SQL lacks NOT NULL, Go is a
// pointer, sql.Null* or omitempty field, JSON Schema is absent from
// "required" or allows "null".
type Field struct {
	Name        string
	Type        LogicalType
	RawType     string
	Nullable    bool
	Enum        []string
	Default     string
	Doc         string
	Annotations Annotations
	Pos         Position
}

// Ref is a reference from some of an entity's fields to another entity: a SQL
// foreign key, a JSON Schema $ref, or a Go field typed as another entity.
// Target is the referenced entity's name as written; linking resolves it.
// Inferred is true for a reference guessed from a name such as customer_id,
// which some designs weigh less.
type Ref struct {
	Fields       []string
	Target       string
	TargetFields []string
	Inferred     bool
	Pos          Position
}

// Check is a declared value constraint on one field: a SQL CHECK with an
// IN-list, a JSON Schema enum or const, or a Go validate oneof tag. Expr holds
// the constraint as written when it is not a plain list.
type Check struct {
	Field string
	Enum  []string
	Expr  string
}

// Entity is one table, view, persisted Go struct, or JSON Schema object
// definition, after every ALTER in the migration sequence has been applied.
//
// ID is unique across the whole input ("sql:public.orders",
// "go:example.com/model.Order", "json:schemas/order.json#/$defs/Line").
// Canonical is the folded name used to link the same logical entity across
// sources (see Canonical); an `entity` annotation overrides it.
type Entity struct {
	ID          string
	Kind        EntityKind
	Source      SourceKind
	Name        string
	Canonical   string
	Doc         string
	Annotations Annotations
	Fields      []Field
	PrimaryKey  []string
	Uniques     [][]string
	Refs        []Ref
	Checks      []Check
	Pos         Position
}

// Problem is a source file the linter could not fully read, such as a SQL
// statement that does not parse. It is reported as an mml/parse-error finding
// rather than aborting the run, so one bad migration does not hide every
// other result.
type Problem struct {
	Message string
	Pos     Position
}

// Source is everything one adapter produced from one file.
type Source struct {
	Entities []Entity
	Queries  []Query
	Problems []Problem
}

// Field returns the field with the given name and true, or nil and false. Names compare
// case-insensitively, because SQL folds unquoted names.
func (e *Entity) Field(name string) (*Field, bool) {
	for i := range e.Fields {
		if strings.EqualFold(e.Fields[i].Name, name) {
			return &e.Fields[i], true
		}
	}
	return nil, false
}

// Keys returns the primary key followed by every UNIQUE key, each as a list of
// field names. An entity with neither returns nil.
func (e *Entity) Keys() [][]string {
	var keys [][]string
	if len(e.PrimaryKey) > 0 {
		keys = append(keys, e.PrimaryKey)
	}
	return append(keys, e.Uniques...)
}
