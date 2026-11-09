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

import (
	"fmt"
	"strings"
)

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
//
// OnDelete is a SQL foreign key's ON DELETE action ("cascade", "restrict",
// "set null", "set default" or "no action"), or "" for a reference no
// database enforces.
//
// ByValue is true when the entity stores a copy of the target rather than
// its key, as a document does: a JSON Schema $ref, or a struct-typed field
// of a BSON document. A SQL foreign key and a gorm association are never by
// value.
type Ref struct {
	Fields       []string
	Target       string
	TargetFields []string
	Inferred     bool
	ByValue      bool
	OnDelete     string
	Pos          Position
}

// Check is a declared value constraint on one field: a SQL CHECK with an
// IN-list, a JSON Schema enum or const, or a Go validate oneof tag. Expr holds
// the constraint as written when it is not a plain list. A CHECK over
// several columns, such as NOT is_synthetic OR generator_version IS NOT
// NULL, has no Field; Columns lists the columns it reads, so a design can
// ask whether two columns are tied together.
type Check struct {
	Field   string
	Enum    []string
	Expr    string
	Columns []string
}

// Grant is what GRANT and REVOKE leave a database role allowed to do with a
// table. Privileges are upper case, with ALL expanded, so a design can ask
// whether a role may INSERT without knowing how the grant was written.
type Grant struct {
	Role       string
	Privileges []string
	Pos        Position
}

// Change is a DDL statement that modifies an existing table rather than
// creating it: Statement is one of "ALTER TABLE", "RENAME", "CREATE INDEX" or
// "CREATE TRIGGER", so a design can report it without reading SQL.
type Change struct {
	Statement string
	Pos       Position
}

// Entity is one table, view, persisted Go struct, or JSON Schema object
// definition, after every ALTER in the migration sequence has been applied.
//
// ID is unique across the whole input ("sql:public.orders",
// "go:example.com/model.Order", "json:schemas/order.json#/$defs/Line").
// Canonical is the folded name used to link the same logical entity across
// sources (see Canonical); an `entity` annotation overrides it.
//
// Indexes lists the columns of every index, unique or not, in index order,
// so a design can ask whether a column leads one. Changes lists the
// statements after the CREATE that modified the table.
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
	Grants      []Grant
	Indexes     [][]string
	Changes     []Change
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

// String formats p as file:line:col, leaving out what is unknown, and as
// <input> when even the file is.
func (p Position) String() string {
	switch {
	case p.File == "":
		return "<input>"
	case p.Line == 0:
		return p.File
	case p.Col == 0:
		return fmt.Sprintf("%s:%d", p.File, p.Line)
	default:
		return fmt.Sprintf("%s:%d:%d", p.File, p.Line, p.Col)
	}
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

// String is the source kind's name in mml.lock.json and messages.
func (s SourceKind) String() string { return sourceNames()[s] }

// String is the entity kind's name in mml.lock.json and messages.
func (k EntityKind) String() string { return kindNames()[k] }

// String is the logical type's name in mml.lock.json and messages.
func (t LogicalType) String() string { return typeNames()[t] }

// sourceNames, kindNames and typeNames are the one table each enum's names
// come from, read both ways, so writing and reading a lock cannot disagree.
func sourceNames() map[SourceKind]string {
	return map[SourceKind]string{SourceSQL: "sql", SourceGo: "go", SourceJSONSchema: "jsonschema"}
}

func kindNames() map[EntityKind]string {
	return map[EntityKind]string{
		KindTable: "table", KindView: "view", KindStruct: "struct", KindSchema: "schema",
	}
}

func typeNames() map[LogicalType]string {
	return map[LogicalType]string{
		TypeUnknown: "unknown", TypeString: "string", TypeInt: "int", TypeDecimal: "decimal",
		TypeFloat: "float", TypeBool: "bool", TypeDate: "date", TypeTimestamptz: "timestamptz",
		TypeTimestampNaive: "timestamp", TypeTime: "time", TypeInterval: "interval",
		TypeUUID: "uuid", TypeBytes: "bytes", TypeJSON: "json", TypeArray: "array",
		TypeObject: "object",
	}
}

// parseName returns the value names gives name, or false.
func parseName[T comparable](names map[T]string, name string) (T, bool) {
	for v, n := range names {
		if n == name {
			return v, true
		}
	}
	var zero T
	return zero, false
}
