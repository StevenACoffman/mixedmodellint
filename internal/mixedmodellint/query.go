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
