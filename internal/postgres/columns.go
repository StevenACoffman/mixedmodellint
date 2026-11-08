package postgres

import (
	"slices"
	"strconv"
	"strings"

	pg "github.com/pganalyze/pg_query_go/v6"

	mml "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// lineIndex maps byte offsets in a file to 1-based line and column numbers.
type lineIndex []int

func newLineIndex(src []byte) lineIndex {
	idx := lineIndex{0}
	for i, b := range src {
		if b == '\n' {
			idx = append(idx, i+1)
		}
	}
	return idx
}

func (li lineIndex) at(offset int) (line, col int) {
	i, found := slices.BinarySearch(li, offset)
	if !found {
		i--
	}
	return i + 1, offset - li[i] + 1
}

// leadingDirectives reads the comment lines at the top of a statement's span
// (pg_query starts a statement right after the previous semicolon, so its
// span includes the comments above it). Lines of the form "-- mml:key value"
// become annotations. It returns the offset of the statement's first code
// character, which is where findings about the statement should point.
func leadingDirectives(f *File, lines lineIndex, start, length int) (int, mml.Annotations) {
	end := len(f.Src)
	if length > 0 && start+length <= end {
		end = start + length
	}
	anns := mml.Annotations{}
	off := start
	for off < end {
		rest := f.Src[off:end]
		trimmed := strings.TrimLeft(string(rest), " \t\r\n")
		off += len(rest) - len(trimmed)
		if !strings.HasPrefix(trimmed, "--") {
			break
		}
		line, _, _ := strings.Cut(trimmed, "\n")
		ln, col := lines.at(off)
		text := strings.TrimSpace(strings.TrimPrefix(line, "--"))
		for k, v := range mml.ParseLines(text, mml.Position{File: f.Path, Line: ln, Col: col}) {
			anns[k] = v
		}
		off += len(line)
	}
	return off, anns
}

func addColumn(ctx *stmtCtx, e *mml.Entity, cd *pg.ColumnDef) {
	if cd == nil {
		return
	}
	f := mml.Field{
		Name:     cd.GetColname(),
		Type:     logicalType(cd.GetTypeName()),
		RawType:  rawType(cd.GetTypeName()),
		Nullable: !cd.GetIsNotNull(),
		Pos:      ctx.pos(int(cd.GetLocation())),
	}
	for _, c := range cd.GetConstraints() {
		con := c.GetConstraint()
		switch con.GetContype() {
		case pg.ConstrType_CONSTR_NOTNULL, pg.ConstrType_CONSTR_PRIMARY:
			f.Nullable = false
		case pg.ConstrType_CONSTR_DEFAULT:
			f.Default = deparseExpr(con.GetRawExpr())
		default:
		}
		addConstraint(e, con, f.Name)
	}
	e.Fields = append(e.Fields, f)
	if slices.ContainsFunc(
		e.PrimaryKey,
		func(k string) bool { return strings.EqualFold(k, f.Name) },
	) {
		e.Fields[len(e.Fields)-1].Nullable = false
	}
}

func dropColumn(e *mml.Entity, name string) {
	e.Fields = slices.DeleteFunc(e.Fields, func(f mml.Field) bool {
		return strings.EqualFold(f.Name, name)
	})
	drop := func(cols []string) bool {
		return slices.ContainsFunc(cols, func(c string) bool { return strings.EqualFold(c, name) })
	}
	if drop(e.PrimaryKey) {
		e.PrimaryKey = nil
	}
	e.Uniques = slices.DeleteFunc(e.Uniques, drop)
	e.Indexes = slices.DeleteFunc(e.Indexes, drop)
	e.Refs = slices.DeleteFunc(e.Refs, func(r mml.Ref) bool { return drop(r.Fields) })
	e.Checks = slices.DeleteFunc(e.Checks, func(c mml.Check) bool {
		return strings.EqualFold(c.Field, name) || containsFold(c.Columns, name)
	})
}

// addConstraint records a constraint on e. column is set for a column
// constraint (id INT PRIMARY KEY), whose key is that column; a table
// constraint lists its own columns.
func addConstraint(e *mml.Entity, con *pg.Constraint, column string) {
	cols := names(con.GetKeys())
	if column != "" {
		cols = []string{column}
	}
	switch con.GetContype() {
	case pg.ConstrType_CONSTR_PRIMARY:
		setPrimaryKey(e, cols)
	case pg.ConstrType_CONSTR_UNIQUE:
		e.Uniques = append(e.Uniques, cols)
	case pg.ConstrType_CONSTR_FOREIGN:
		e.Refs = append(e.Refs, foreignKey(con, column))
	case pg.ConstrType_CONSTR_CHECK:
		if ch, ok := readCheck(con.GetRawExpr()); ok {
			e.Checks = append(e.Checks, ch)
		}
	default:
	}
}

// foreignKey reads a FOREIGN KEY or REFERENCES constraint; column is set for
// a column constraint, whose referencing column is that column.
func foreignKey(con *pg.Constraint, column string) mml.Ref {
	fk := names(con.GetFkAttrs())
	if column != "" {
		fk = []string{column}
	}
	actions := map[string]string{
		"a": "no action", "r": "restrict", "c": "cascade", "n": "set null", "d": "set default",
	}
	return mml.Ref{
		Fields: fk, Target: tableKey(con.GetPktable()), TargetFields: names(con.GetPkAttrs()),
		OnDelete: actions[con.GetFkDelAction()],
	}
}

// setPrimaryKey records cols as e's primary key, which also makes them NOT
// NULL, as PostgreSQL does.
func setPrimaryKey(e *mml.Entity, cols []string) {
	e.PrimaryKey = cols
	for i := range e.Fields {
		if containsFold(cols, e.Fields[i].Name) {
			e.Fields[i].Nullable = false
		}
	}
}

func containsFold(list []string, s string) bool {
	return slices.ContainsFunc(list, func(x string) bool { return strings.EqualFold(x, s) })
}

// readCheck reads a CHECK expression in the most specific form that fits:
// an IN-list, a comparison of one column with a constant, or the columns of
// an expression over several columns. A CHECK on one column that is none of
// these says nothing a design reads, so it is not kept.
func readCheck(n *pg.Node) (mml.Check, bool) {
	if ch, ok := inListCheck(n); ok {
		return ch, true
	}
	if ch, ok := comparisonCheck(n); ok {
		return ch, true
	}
	if cols := checkColumns(n); len(cols) >= 2 {
		return mml.Check{Columns: cols}, true
	}
	return mml.Check{}, false
}

// checkColumns returns the columns a CHECK expression reads, each once, in
// the order they first appear.
func checkColumns(n *pg.Node) []string {
	var cols []string
	walkAll(n, func(m *pg.Node) {
		if c := columnName(m); c != "" && !containsFold(cols, c) {
			cols = append(cols, c)
		}
	})
	return cols
}

// inListCheck recognizes CHECK (col IN ('a', 'b')), the form §7.4-B and §5.1
// read as an enumeration.
// comparisonCheck reads a CHECK that compares one column with a constant,
// such as qty > 0, or bounds it with BETWEEN, into a Check whose Expr is the
// comparison (a BETWEEN as its two comparisons). Anything else is not kept.
func comparisonCheck(n *pg.Node) (mml.Check, bool) {
	ae := n.GetAExpr()
	if ae == nil {
		return mml.Check{}, false
	}
	col := columnName(ae.GetLexpr())
	if col == "" {
		return mml.Check{}, false
	}
	switch ae.GetKind() {
	case pg.A_Expr_Kind_AEXPR_OP:
		switch op := opName(ae); op {
		case "<", "<=", ">", ">=", "<>", "!=":
			return mml.Check{
				Field: col,
				Expr:  col + " " + op + " " + deparseExpr(ae.GetRexpr()),
			}, true
		}
	case pg.A_Expr_Kind_AEXPR_BETWEEN:
		bounds := ae.GetRexpr().GetList().GetItems()
		if len(bounds) == 2 {
			return mml.Check{Field: col, Expr: col + " >= " + deparseExpr(bounds[0]) + " AND " +
				col + " <= " + deparseExpr(bounds[1])}, true
		}
	default:
	}
	return mml.Check{}, false
}

func inListCheck(n *pg.Node) (mml.Check, bool) {
	ae := n.GetAExpr()
	if ae == nil || ae.GetKind() != pg.A_Expr_Kind_AEXPR_IN {
		return mml.Check{}, false
	}
	col := columnName(ae.GetLexpr())
	if col == "" {
		return mml.Check{}, false
	}
	var values []string
	for _, item := range ae.GetRexpr().GetList().GetItems() {
		if s := item.GetAConst().GetSval(); s != nil {
			values = append(values, s.GetSval())
		}
	}
	return mml.Check{Field: col, Enum: values}, len(values) > 0
}

func columnName(n *pg.Node) string {
	fields := n.GetColumnRef().GetFields()
	if len(fields) == 0 {
		return ""
	}
	return fields[len(fields)-1].GetString_().GetSval()
}

func names(nodes []*pg.Node) []string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n.GetString_().GetSval())
	}
	return out
}

// typeName returns the last part of a type's name: pg_catalog.int4 is int4.
func typeName(tn *pg.TypeName) string {
	parts := tn.GetNames()
	if len(parts) == 0 {
		return ""
	}
	return strings.ToLower(parts[len(parts)-1].GetString_().GetSval())
}

// logicalType maps a PostgreSQL type to the IR's logical type. The parser has
// already normalized the SQL spellings: INTEGER is int4, TIMESTAMP WITH TIME
// ZONE is timestamptz, plain TIMESTAMP is timestamp.
func logicalType(tn *pg.TypeName) mml.LogicalType {
	if len(tn.GetArrayBounds()) > 0 {
		return mml.TypeArray
	}
	types := map[string]mml.LogicalType{
		"int2": mml.TypeInt, "int4": mml.TypeInt, "int8": mml.TypeInt, "serial": mml.TypeInt,
		"bigserial": mml.TypeInt, "smallserial": mml.TypeInt,
		"numeric": mml.TypeDecimal, "money": mml.TypeDecimal,
		"float4": mml.TypeFloat, "float8": mml.TypeFloat,
		"bool": mml.TypeBool, "date": mml.TypeDate,
		"timestamptz": mml.TypeTimestamptz, "timestamp": mml.TypeTimestampNaive,
		"time": mml.TypeTime, "timetz": mml.TypeTime, "interval": mml.TypeInterval,
		"uuid": mml.TypeUUID, "bytea": mml.TypeBytes, "json": mml.TypeJSON, "jsonb": mml.TypeJSON,
		"text": mml.TypeString, "varchar": mml.TypeString, "bpchar": mml.TypeString,
		"char": mml.TypeString, "citext": mml.TypeString,
	}
	return types[typeName(tn)]
}

// rawType renders a type as written, with its modifiers: varchar(10),
// numeric(12,2), int4[].
func rawType(tn *pg.TypeName) string {
	var mods []string
	for _, m := range tn.GetTypmods() {
		if iv := m.GetAConst().GetIval(); iv != nil {
			mods = append(mods, strconv.Itoa(int(iv.GetIval())))
		}
	}
	s := typeName(tn)
	if len(mods) > 0 {
		s += "(" + strings.Join(mods, ",") + ")"
	}
	return s + strings.Repeat("[]", len(tn.GetArrayBounds()))
}

// deparseExpr renders a default expression for the IR, or "" when it is not
// a simple constant.
func deparseExpr(n *pg.Node) string {
	c := n.GetAConst()
	switch {
	case c == nil:
		return ""
	case c.GetSval() != nil:
		return c.GetSval().GetSval()
	case c.GetIval() != nil:
		return strconv.Itoa(int(c.GetIval().GetIval()))
	default:
		return ""
	}
}
