package postgres

import (
	"strings"

	pg "github.com/pganalyze/pg_query_go/v6"

	mml "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// assigner works out where the values a write sets come from. relations are
// the FROM items in scope, used to turn a column's alias into a relation name;
// args are the origins of the Go values bound to $1, $2, ... when the
// statement is a snippet.
type assigner struct {
	relations []mml.Relation
	args      [][]mml.Origin
	pos       mml.Position
}

// valuesAssignments returns one assignment per column of an INSERT ...
// VALUES, merging the origins of every row.
//
// Requires: every row has the same length as cols (the parser enforces it).
func (a *assigner) valuesAssignments(cols []string, rows []*pg.Node) []mml.Assignment {
	out := a.unknown(cols)
	for _, row := range rows {
		for i, v := range row.GetList().GetItems() {
			if i < len(out) {
				out[i].Origins = append(out[i].Origins, a.origins(v)...)
			}
		}
	}
	return out
}

// selectAssignments returns one assignment per column of an INSERT ...
// SELECT, from the SELECT's target list in order.
func (a *assigner) selectAssignments(cols []string, targets []*pg.Node) []mml.Assignment {
	out := a.unknown(cols)
	for i, t := range targets {
		if i < len(out) {
			out[i].Origins = a.origins(t.GetResTarget().GetVal())
		}
	}
	return out
}

// setAssignments returns the columns of an UPDATE's SET list.
func (a *assigner) setAssignments(targets []*pg.Node) []mml.Assignment {
	var out []mml.Assignment
	for _, t := range targets {
		rt := t.GetResTarget()
		out = append(out, mml.Assignment{
			Column: rt.GetName(), Origins: a.origins(rt.GetVal()), Pos: a.pos,
		})
	}
	return out
}

// unknown returns an assignment per column with nothing yet known of its
// origins.
func (a *assigner) unknown(cols []string) []mml.Assignment {
	out := make([]mml.Assignment, len(cols))
	for i, c := range cols {
		out[i] = mml.Assignment{Column: c, Pos: a.pos}
	}
	return out
}

// origins returns how the value of expression n arises.
//
// Ensures: a column of a stored relation is a copy of that relation's field;
// a bind parameter has the origins of the Go value bound to it; date_trunc
// and casts to date or to a timestamp with fewer than six fractional digits
// add a truncation to the origins of their argument; any other expression
// returns nil, meaning nothing is known.
func (a *assigner) origins(n *pg.Node) []mml.Origin {
	switch {
	case n.GetParamRef() != nil:
		i := int(n.GetParamRef().GetNumber()) - 1
		if i >= 0 && i < len(a.args) {
			return append([]mml.Origin(nil), a.args[i]...)
		}
	case n.GetColumnRef() != nil:
		if from, ok := a.field(columnRef(n.GetColumnRef(), defaultRelation(a.relations))); ok {
			return []mml.Origin{{From: from}}
		}
	case n.GetFuncCall() != nil:
		fc := n.GetFuncCall()
		if funcName(fc) == "date_trunc" && len(fc.GetArgs()) == 2 {
			return truncated(a.origins(fc.GetArgs()[1]), constString(fc.GetArgs()[0]))
		}
	case n.GetTypeCast() != nil:
		tc := n.GetTypeCast()
		return truncated(a.origins(tc.GetArg()), castUnit(tc.GetTypeName()))
	}
	return nil
}

// field resolves a column reference to the stored field it names. A column of
// a CTE or subquery, or one whose relation is not in scope, resolves to
// nothing rather than to a guess.
func (a *assigner) field(c mml.ColumnRef) (mml.FieldRef, bool) {
	for _, r := range a.relations {
		if strings.EqualFold(r.Alias, c.Relation) && !r.Derived {
			return mml.FieldRef{Entity: r.Name, Field: c.Column}, true
		}
	}
	return mml.FieldRef{}, false
}

// truncated marks origins, in place, as truncated to unit; with no known origin, the
// truncation is still known. An empty unit leaves origins as they are.
func truncated(origins []mml.Origin, unit string) []mml.Origin {
	if unit == "" {
		return origins
	}
	if len(origins) == 0 {
		return []mml.Origin{{Truncate: unit}}
	}
	for i := range origins {
		origins[i].Truncate = unit
	}
	return origins
}

// castUnit is the time unit a cast truncates to: a day for ::date, a second
// for ::timestamp(0), a millisecond for ::timestamp(1) to (3). Other casts,
// and timestamps with the default six digits, truncate nothing.
func castUnit(tn *pg.TypeName) string {
	switch typeName(tn) {
	case "date":
		return "day"
	case "timestamp", "timestamptz":
	default:
		return ""
	}
	mods := tn.GetTypmods()
	if len(mods) == 0 {
		return ""
	}
	iv := mods[0].GetAConst().GetIval()
	if iv == nil {
		return ""
	}
	switch p := iv.GetIval(); {
	case p == 0:
		return "second"
	case p <= 3:
		return "millisecond"
	default:
		return ""
	}
}
