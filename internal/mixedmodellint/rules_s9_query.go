package mixedmodellint

import (
	"fmt"
	"slices"
)

// check91B applies §9.1-B: an aggregation that buckets or filters by when a
// row was loaded or processed, while its entity also records when the event
// happened, puts late-arriving rows in the wrong period.
func check91B(c *catalog) []issue {
	var out []issue
	for i := range c.queries {
		q := &c.queries[i]
		if len(q.Aggregates) == 0 && len(q.GroupBy) == 0 {
			continue
		}
		s := c.scopeOf(q)
		var seen []*Field
		for _, ref := range reportingColumns(q) {
			e, f := s.resolve(ref)
			if f == nil || slices.Contains(seen, f) || !systemTime(f) {
				continue
			}
			event := eventTimeField(e)
			if event == nil {
				continue
			}
			seen = append(seen, f)
			out = append(out, issue{
				entity: e, field: f.Name, pos: q.Pos,
				measured: fmt.Sprintf("the query buckets or filters by %s.%s, which is when a "+
					"system saw each row, not when it happened", e.Name, f.Name),
				suggestion: fmt.Sprintf("Use %[1]s instead, for example date_trunc('day', "+
					"%[1]s): by %[2]s, late-arriving rows land in the wrong period. A freshness "+
					"monitor declares waive (§9.1).", event.Name, f.Name),
			})
		}
	}
	return out
}

// reportingColumns returns the columns q groups, filters or truncates by.
func reportingColumns(q *Query) []ColumnRef {
	out := slices.Concat(q.GroupBy, q.Filters)
	for _, te := range q.TimeExprs {
		out = append(out, te.Column)
	}
	return out
}

// systemTime reports whether f is a date or timestamp of ingestion or
// processing (§9.1-A's classification).
func systemTime(f *Field) bool {
	r := temporalRole(f)
	return isTemporal(f.Type) && (r == roleIngested || r == roleProcessed)
}
