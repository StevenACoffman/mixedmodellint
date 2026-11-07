package mixedmodellint

import (
	"fmt"
	"strings"
)

// check53B applies §5.3-B: an aggregate skips NULLs, so over a column whose
// NULL means zero it answers the wrong question, and over a nullable column
// that says nothing about its NULLs the answer is ambiguous. The adapter
// only gives an aggregate a column argument when the argument is a bare
// column, so a COALESCE, NULLIF or CASE around it already passes.
//
// Deviation: rule_plan.md's case 2, arithmetic on a nullable operand, needs
// expressions the query model does not keep yet.
func check53B(c *catalog) []issue {
	var out []issue
	for i := range c.queries {
		q := &c.queries[i]
		s := c.scopeOf(q)
		for j := range q.Aggregates {
			if is, ok := nullSkippingIssue(s, &q.Aggregates[j]); ok {
				out = append(out, is)
			}
		}
	}
	return out
}

// nullSkippingIssue grades one aggregate: avg, sum, min or max of a
// null=zero column is an error, and so is count of one, which counts rows
// with a value as if NULL rows had none; any of the four over a nullable
// column with no null= declared is a warning.
func nullSkippingIssue(s scope, a *Aggregate) (issue, bool) {
	e, f := nullableArgument(s, a)
	if f == nil {
		return issue{}, false
	}
	meaning, declared := f.Annotations.Get("null")
	call := fmt.Sprintf("%s(%s)", strings.ToUpper(a.Func), a.Arg.Column)
	is := issue{entity: e, field: f.Name, pos: a.Pos}
	switch {
	case meaning == "zero" && a.Func == "count":
		is.measured = fmt.Sprintf("%s skips NULLs, but %s.%s declares that NULL means zero, "+
			"so rows recorded as zero are not counted", call, e.Name, f.Name)
		is.suggestion = "Use COUNT(*) for a row count, or COUNT(*) FILTER (WHERE " +
			a.Arg.Column + " <> 0) for rows with a non-zero value (§5.3)."
	case meaning == "zero" && aggregatesValues(a.Func):
		is.measured = fmt.Sprintf("%s skips NULLs, but %s.%s declares that NULL means zero, "+
			"so %s", call, e.Name, f.Name, zeroSkipped(a.Func))
		is.suggestion = fmt.Sprintf("Use %s(COALESCE(%s, 0)) (§5.3).", strings.ToUpper(a.Func),
			a.Arg.Column)
	case !declared && aggregatesValues(a.Func):
		is.level = SeverityWarning
		is.measured = fmt.Sprintf("%s skips NULLs of %s.%s, which does not declare what NULL "+
			"means, so whether that is right is ambiguous", call, e.Name, f.Name)
		is.suggestion = fmt.Sprintf("Declare null= on %s.%s so this aggregate can be "+
			"checked (§5.3).", e.Name, f.Name)
	default:
		return issue{}, false
	}
	return is, true
}

// nullableArgument returns the stored field a non-DISTINCT aggregate of a
// bare column reads, when it may be NULL; otherwise nil. COUNT(*) and an
// argument that is not a bare column have no column to resolve.
func nullableArgument(s scope, a *Aggregate) (*Entity, *Field) {
	if a.Distinct {
		return nil, nil
	}
	e, f := s.resolve(a.Arg)
	if f == nil || !f.Nullable {
		return nil, nil
	}
	return e, f
}

// zeroSkipped says what skipping NULLs that mean zero does to fn's result.
func zeroSkipped(fn string) string {
	switch fn {
	case "avg":
		return "the average leaves the zeros out of the count"
	case "sum":
		return "a group whose rows are all NULL sums to NULL instead of 0"
	case "min":
		return "the zeros are never the minimum"
	default:
		return "the zeros are never the maximum"
	}
}

func aggregatesValues(fn string) bool {
	return fn == "avg" || fn == "sum" || fn == "min" || fn == "max"
}
