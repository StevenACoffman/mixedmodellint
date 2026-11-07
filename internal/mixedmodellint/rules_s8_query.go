package mixedmodellint

import (
	"fmt"
	"strings"
)

// check83B applies §8.3-B: SUM over a semi-additive measure (a balance, a
// stock level) is meaningless along the dimension it is semi-additive over,
// so the query must either group by that dimension or pin it in WHERE. A
// field declared measure=semi_additive names it with over=; an undeclared
// field the §8.3 lexicon calls semi-additive is checked against its
// entity's time key, one severity lower.
//
// Deviation: the query model does not say how WHERE tests a column, so any
// filter on the dimension counts as pinning it; window aggregates are not
// checked.
func check83B(c *catalog) []issue {
	var out []issue
	for i := range c.queries {
		q := &c.queries[i]
		s := c.scopeOf(q)
		for j := range q.Aggregates {
			a := &q.Aggregates[j]
			if a.Func != "sum" {
				continue
			}
			if is, ok := semiAdditiveSum(s, a); ok {
				out = append(out, is)
			}
		}
	}
	return out
}

// semiAdditiveSum reports a SUM that collapses the over= dimension of its
// semi-additive argument.
func semiAdditiveSum(s scope, a *Aggregate) (issue, bool) {
	e, f := s.resolve(a.Arg)
	if f == nil {
		return issue{}, false
	}
	over, level, ok := semiAdditiveOver(e, f)
	if !ok || groupsBy(s.q, over) || filtersOn(s.q, over) {
		return issue{}, false
	}
	is := issue{level: level, entity: e, field: f.Name, pos: a.Pos}
	is.measured = fmt.Sprintf("SUM(%s) adds %s.%s across %s, but %s is semi-additive over "+
		"%s", a.Arg.Column, e.Name, f.Name, over, f.Name, over)
	is.suggestion = fmt.Sprintf("Sum across the other dimensions for one %[1]s (WHERE %[1]s "+
		"= $1), or take each key's latest value (DISTINCT ON (<key>) ... ORDER BY <key>, %[1]s "+
		"DESC) (§8.3).", over)
	return is, true
}

// semiAdditiveOver returns the dimension f is semi-additive over and the
// severity a SUM across it gets: f's over= when f declares
// measure=semi_additive, or the time key of e, one level lower, when f is
// undeclared and the §8.3 lexicon calls it semi-additive.
func semiAdditiveOver(e *Entity, f *Field) (over string, level Severity, ok bool) {
	measure, declared := f.Annotations.Get("measure")
	switch {
	case measure == "semi_additive":
		over, _ = f.Annotations.Get("over")
		return over, 0, over != ""
	case !declared && measureLexicon(f.Name) == "semi_additive" && len(keyTimeColumn(e)) == 1:
		return keyTimeColumn(e)[0], SeverityWarning, true
	default:
		return "", 0, false
	}
}

func groupsBy(q *Query, col string) bool {
	for _, g := range q.GroupBy {
		if strings.EqualFold(g.Column, col) {
			return true
		}
	}
	return false
}

// check84B applies §8.4-B: SUM or AVG over a stored value that cannot be
// re-aggregated (an average, a ratio, a distinct count) gives every group's
// value equal weight. A field declared measure=non_additive is an error; a
// field only named like one (§8.4-A's words) a warning.
//
// Deviation: the plan's case (b), re-aggregating a non-decomposable output
// of a CTE, subquery or view, needs the select list of derived relations,
// which the query model does not keep yet.
func check84B(c *catalog) []issue {
	var out []issue
	for i := range c.queries {
		q := &c.queries[i]
		s := c.scopeOf(q)
		for j := range q.Aggregates {
			if is, ok := reaggregated(s, &q.Aggregates[j]); ok {
				out = append(out, is)
			}
		}
	}
	return out
}

// reaggregated reports a SUM or AVG of a non-decomposable field.
func reaggregated(s scope, a *Aggregate) (issue, bool) {
	if a.Func != "sum" && a.Func != "avg" {
		return issue{}, false
	}
	e, f := s.resolve(a.Arg)
	if f == nil {
		return issue{}, false
	}
	is := issue{entity: e, field: f.Name, pos: a.Pos}
	switch measure, declared := f.Annotations.Get("measure"); {
	case measure == "non_additive":
	case !declared && nonDecomposableName(f.Name):
		is.level = SeverityWarning
	default:
		return issue{}, false
	}
	is.measured = fmt.Sprintf("%s(%s) re-aggregates %s.%s, which cannot be summed or "+
		"averaged again: every group gets equal weight", strings.ToUpper(a.Func),
		a.Arg.Column, e.Name, f.Name)
	is.suggestion = fmt.Sprintf("Recompute from the components over the atomic rows, such "+
		"as SUM(<numerator>)::numeric / NULLIF(SUM(<denominator>), 0), or COUNT(DISTINCT "+
		"<key>), instead of re-aggregating %s (§8.4).", f.Name)
	return is, true
}

func nonDecomposableName(name string) bool {
	for _, w := range splitWords(name) {
		if nonDecomposableWord(w) {
			return true
		}
	}
	return false
}
