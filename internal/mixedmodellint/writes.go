package mixedmodellint

import "strings"

// write is one assignment of the query corpus, resolved: the entity written,
// the field set (nil when the entity has no such field), and the assignment.
type write struct {
	entity     *Entity
	field      *Field
	assignment *Assignment
}

// writes returns every assignment in the corpus to an entity in the catalog.
// A write to a table defined outside the repository has no entity and is
// left out.
func (c *catalog) writes() []write {
	var out []write
	for i := range c.queries {
		q := &c.queries[i]
		switch q.Kind {
		case QueryInsert, QueryUpdate, QueryMerge:
		default:
			continue
		}
		e := c.lookup(q.Target, SourceSQL)
		if e == nil {
			continue
		}
		for j := range q.Assignments {
			a := &q.Assignments[j]
			f, _ := e.Field(a.Column)
			out = append(out, write{entity: e, field: f, assignment: a})
		}
	}
	return out
}

// at is where an origin's evidence is: the Go line that copies or truncates
// the value, or else the write itself.
func (w write) at(o *Origin) Position {
	if o.Pos != (Position{}) {
		return o.Pos
	}
	return w.assignment.Pos
}

// writtenUnit returns the coarsest time unit every write to e's column col
// keeps: when each write in the corpus truncates the value, the finest of
// their units, with the position of a write that truncates to it.
//
// Requires: c's corpus holds every write to e that the repository makes.
// Ensures:  ok is false when the column is never written, or when any write
// has an origin that is unknown or not truncated; one untruncated write means
// the column can hold full precision.
func (c *catalog) writtenUnit(e *Entity, col string) (unit string, at Position, ok bool) {
	for _, w := range c.writes() {
		if w.entity != e || !strings.EqualFold(w.assignment.Column, col) {
			continue
		}
		if len(w.assignment.Origins) == 0 {
			return "", Position{}, false
		}
		for i := range w.assignment.Origins {
			o := &w.assignment.Origins[i]
			r := timeUnitRank(o.Truncate)
			if r == 0 {
				return "", Position{}, false
			}
			if unit == "" || r < timeUnitRank(unit) {
				unit, at = o.Truncate, w.at(o)
			}
		}
	}
	return unit, at, unit != ""
}

// ancestors returns every entity e reaches by references, each once, nearest
// first.
func (c *catalog) ancestors(e *Entity) []*Entity {
	var out []*Entity
	seen := map[*Entity]bool{e: true}
	queue := []*Entity{e}
	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]
		for _, p := range c.parents(next) {
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
				queue = append(queue, p)
			}
		}
	}
	return out
}

// eventTime reports whether f holds when something happened, as rule_plan.md
// §9.1-A classifies temporal fields: a time= annotation decides; otherwise a
// timestamp named with an at/time/ts stem is event time unless its words say
// ingestion, processing, or row bookkeeping (created_at is ambiguous: in the
// store it is when the row was written).
func eventTime(f *Field) bool {
	if f.Type != TypeTimestamptz && f.Type != TypeTimestampNaive {
		return false
	}
	if t, ok := f.Annotations.Get("time"); ok {
		return t == "event"
	}
	ws := splitWords(f.Name)
	if len(ws) == 0 {
		return false
	}
	switch ws[len(ws)-1] {
	case "at", "time", "ts", "timestamp":
	default:
		return false
	}
	for _, w := range ws {
		switch w {
		case "ingest", "ingested", "loaded", "received", "arrived", "landed", "processed",
			"transformed", "computed", "etl", "batch", "run", "created", "inserted", "updated",
			"modified", "deleted", "synced":
			return false
		}
	}
	return true
}

// finestKept returns the finest time unit a write may truncate f to without
// losing precision the source had: the unit of a declared precision= (ns,
// us, ms or s), or else a millisecond, since rule_plan.md §9.8-B reports
// truncation to a second or coarser when nothing is declared.
func finestKept(f *Field) string {
	p, _ := f.Annotations.Get("precision")
	switch strings.ToLower(p) {
	case "ns":
		return "nanosecond"
	case "us":
		return "microsecond"
	case "s":
		return "second"
	default:
		return "millisecond"
	}
}
