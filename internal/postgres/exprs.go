package postgres

import (
	"strings"

	pg "github.com/pganalyze/pg_query_go/v6"
	"google.golang.org/protobuf/reflect/protoreflect"

	mml "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// walk calls visit on n and every node below it, except inside a nested
// SELECT: a subquery is its own Query, so its aggregates and time expressions
// must not be attributed to the enclosing one.
func walk(n *pg.Node, visit func(*pg.Node)) {
	if n == nil {
		return
	}
	traverse(n.ProtoReflect(), visit, true)
}

// walkAll is walk without the subquery boundary.
func walkAll(n *pg.Node, visit func(*pg.Node)) {
	if n == nil {
		return
	}
	traverse(n.ProtoReflect(), visit, false)
}

// traverse visits every *pg.Node reachable from m through message fields,
// using protobuf reflection so no node type can be forgotten.
func traverse(m protoreflect.Message, visit func(*pg.Node), stopAtSelect bool) {
	if node, ok := m.Interface().(*pg.Node); ok {
		if stopAtSelect && node.GetSelectStmt() != nil {
			return
		}
		visit(node)
	}
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		switch {
		case fd.IsMap() || fd.Message() == nil:
		case fd.IsList():
			list := v.List()
			for i := range list.Len() {
				traverse(list.Get(i).Message(), visit, stopAtSelect)
			}
		default:
			traverse(v.Message(), visit, stopAtSelect)
		}
		return true
	})
}

func opName(ae *pg.A_Expr) string {
	if ae.GetKind() != pg.A_Expr_Kind_AEXPR_OP {
		return ""
	}
	parts := ae.GetName()
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1].GetString_().GetSval()
}

func isOne(n *pg.Node) bool {
	iv := n.GetAConst().GetIval()
	return iv != nil && iv.GetIval() == 1
}

// columnRef converts a parse-tree column reference. An unqualified column is
// attributed to scope, the only relation in the query when there is one.
func columnRef(cr *pg.ColumnRef, scope string) mml.ColumnRef {
	var parts []string
	for _, f := range cr.GetFields() {
		if s := f.GetString_(); s != nil {
			parts = append(parts, s.GetSval())
		}
	}
	switch len(parts) {
	case 0:
		return mml.ColumnRef{}
	case 1:
		return mml.ColumnRef{Relation: scope, Column: parts[0]}
	default:
		return mml.ColumnRef{Relation: parts[len(parts)-2], Column: parts[len(parts)-1]}
	}
}

func columnRefs(nodes []*pg.Node, scope string) []mml.ColumnRef {
	var out []mml.ColumnRef
	for _, n := range nodes {
		if cr := unwrapCast(n).GetColumnRef(); cr != nil {
			out = append(out, columnRef(cr, scope))
		}
	}
	return out
}

// defaultRelation is the alias an unqualified column belongs to: the only
// relation in scope, or none.
func defaultRelation(rels []mml.Relation) string {
	if len(rels) == 1 {
		return rels[0].Alias
	}
	return ""
}

func funcName(fc *pg.FuncCall) string {
	parts := fc.GetFuncname()
	if len(parts) == 0 {
		return ""
	}
	return strings.ToLower(parts[len(parts)-1].GetString_().GetSval())
}

func isAggregate(name string) bool {
	switch name {
	case "sum", "avg", "count", "min", "max", "any_value", "percentile_cont",
		"percentile_disc", "mode", "median", "stddev", "variance", "array_agg",
		"string_agg", "bool_and", "bool_or":
		return true
	default:
		return false
	}
}

// unwrapCast returns the expression inside a cast: revenue::numeric is revenue.
func unwrapCast(n *pg.Node) *pg.Node {
	for n.GetTypeCast() != nil {
		n = n.GetTypeCast().GetArg()
	}
	return n
}

func constString(n *pg.Node) string {
	if s := unwrapCast(n).GetAConst().GetSval(); s != nil {
		return strings.ToLower(s.GetSval())
	}
	return ""
}

// timeOfDayCompare recognizes a comparison of a column with a timestamp
// literal that has a time of day, such as placed_on >= '2026-06-03 09:00'.
func timeOfDayCompare(ae *pg.A_Expr, scope string) (mml.TimeExpr, bool) {
	switch opName(ae) {
	case "=", "<", ">", "<=", ">=", "<>":
	default:
		return mml.TimeExpr{}, false
	}
	col, lit := unwrapCast(ae.GetLexpr()).GetColumnRef(), constString(ae.GetRexpr())
	if col == nil {
		col, lit = unwrapCast(ae.GetRexpr()).GetColumnRef(), constString(ae.GetLexpr())
	}
	if col == nil || !hasTimeOfDay(lit) {
		return mml.TimeExpr{}, false
	}
	return mml.TimeExpr{Func: "compare", Unit: "hour", Column: columnRef(col, scope)}, true
}

// hasTimeOfDay reports whether s starts like "2026-06-03 09:00" or
// "2026-06-03T09:00".
func hasTimeOfDay(s string) bool {
	if len(s) < 16 || s[4] != '-' || s[7] != '-' || (s[10] != ' ' && s[10] != 't') || s[13] != ':' {
		return false
	}
	for _, i := range []int{0, 1, 2, 3, 5, 6, 8, 9, 11, 12, 14, 15} {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
