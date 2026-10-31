package mixedmodellint

// Query kinds.
const (
	QuerySelect QueryKind = iota + 1
	QueryInsert
	QueryUpdate
	QueryDelete
	QueryMerge
	QueryCreateView
)

// Dedup kinds.
const (
	DedupWindow DedupKind = iota + 1
	DedupDistinctOn
	DedupDistinct
	DedupGroupBy
)

// Query is one SQL statement from the query corpus, reduced to the facts the
// Query-family designs read. The postgres adapter builds it from the parse
// tree; the core never sees a parser type, so a second dialect only needs a
// second adapter (rule_plan.md §1, "Dialect interface").
//
// Each SELECT level is its own Query: a CTE or subquery is a separate Query,
// and the enclosing one sees it as a Relation with Derived set. A design that
// needs a stored entity therefore skips derived relations instead of guessing
// what they contain.
type Query struct {
	Pos  Position
	Kind QueryKind
	// Target is the entity written by an INSERT, UPDATE or MERGE, or the view
	// created by CREATE VIEW. Empty for a plain SELECT.
	Target     string
	Relations  []Relation
	Joins      []Join
	Aggregates []Aggregate
	GroupBy    []ColumnRef
	Dedups     []Dedup
	TimeExprs  []TimeExpr
	// ConflictKey is the column list of ON CONFLICT (...) or of a MERGE ON
	// conjunction of equalities. Nil when the write has neither.
	ConflictKey []string
	// FromSelect is true for INSERT ... SELECT, which copies rows.
	FromSelect bool
	// Filters are the columns the WHERE clause tests.
	Filters []ColumnRef
	// Assignments are the columns an INSERT with a column list or an UPDATE
	// sets, in statement order, with what is known of each value's origin.
	Assignments []Assignment
}

// QueryKind is the statement type.
type QueryKind int

// Relation is one FROM item. Alias is what columns are qualified with; it
// equals Name when the query gives no alias.
type Relation struct {
	Name    string
	Alias   string
	Derived bool
}

// ColumnRef is a column reference. Relation is the alias or table name it was
// qualified with, or empty when the query did not qualify it and the adapter
// could not attribute it to the only relation in scope.
type ColumnRef struct {
	Relation string
	Column   string
}

// Join is one join edge: the relation joined in (Right, by alias) and the
// column equalities of its ON or USING clause. Left columns may come from any
// relation already in scope.
type Join struct {
	Right string
	On    []ColumnPair
	Pos   Position
}

// ColumnPair is one equality in a join condition.
type ColumnPair struct {
	Left  ColumnRef
	Right ColumnRef
}

// Aggregate is one aggregate function call. Arg is the zero ColumnRef for
// COUNT(*) (Star) or for an argument that is not a plain column.
type Aggregate struct {
	Func     string
	Arg      ColumnRef
	Star     bool
	Distinct bool
	Pos      Position
}

// Dedup is a read that removes duplicate rows: ROW_NUMBER()/RANK() OVER
// (PARTITION BY ...) = 1, DISTINCT ON, SELECT DISTINCT, or GROUP BY with only
// MIN/MAX/ANY_VALUE aggregates. Partition holds the columns rows are made
// unique on. OrderBy holds the window's ORDER BY columns, and Descending is
// true when the first of them sorts descending, which identifies a
// latest-version read.
type Dedup struct {
	Kind       DedupKind
	Partition  []ColumnRef
	OrderBy    []ColumnRef
	Descending bool
	Pos        Position
}

// DedupKind is how a Dedup removes duplicates.
type DedupKind int

// TimeExpr is an expression that asks for a particular time unit from a
// column: date_trunc('hour', c), EXTRACT(HOUR FROM c), date_part('hour', c),
// or a comparison of c with a literal that has a time of day, which the
// adapter reports with Func "compare" and Unit "hour".
type TimeExpr struct {
	Func   string
	Unit   string
	Column ColumnRef
	Pos    Position
}

// Assignment is one column a write sets. Origins holds every way the written
// value is known to arise; it is empty when nothing is known, which every
// design treats as no evidence either way.
type Assignment struct {
	Column  string
	Origins []Origin
	Pos     Position
}

// Origin is one way a written value arises: as a copy of a stored field, as a
// time truncated to a unit, or both (date_trunc('day', o.placed_at)).
type Origin struct {
	// From is the stored field the value copies; zero when it is not a copy.
	From FieldRef
	// Truncate is the time unit the value was truncated to before the write,
	// such as "second"; empty when it was not truncated.
	Truncate string
	// Pos is where the copy or truncation happens, which for Go code is a line
	// of Go rather than the SQL. Zero means the write itself.
	Pos Position
}

// FieldRef names a stored field by its entity and column name. Unlike a
// ColumnRef, whose Relation is an alias that only means something inside one
// query, Entity is a name the catalog can look up.
type FieldRef struct {
	Entity string
	Field  string
}
