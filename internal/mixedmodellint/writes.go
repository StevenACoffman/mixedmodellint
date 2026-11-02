package mixedmodellint

import "strings"

// Time roles.
const (
	roleNone timeRole = iota
	roleEvent
	roleIngested
	roleProcessed
	roleAmbiguous
	roleConflict
)

// timeRole is what a temporal field records (rule_plan.md §9.1-A).
type timeRole int

// write is one assignment of the query corpus, resolved: the entity written,
// the field set (nil when the entity has no such field), the assignment, and
// the kind of statement that makes it.
type write struct {
	entity     *Entity
	field      *Field
	assignment *Assignment
	kind       QueryKind
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
			out = append(out, write{entity: e, field: f, assignment: a, kind: q.Kind})
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

// eventTime reports whether f is a timestamp holding when something
// happened (rule_plan.md §9.1-A), the kind of field truncation must not
// touch.
func eventTime(f *Field) bool {
	return (f.Type == TypeTimestamptz || f.Type == TypeTimestampNaive) &&
		temporalRole(f) == roleEvent
}

// temporalRole classifies a temporal field by what its time records, as
// rule_plan.md §9.1-A does: a time= annotation decides; otherwise the words
// of its name. created_at is ambiguous: in the store it is when the row was
// written, which may or may not be when the thing happened. A name whose
// words point two ways (loaded_event_at) is a conflict, and a name with none
// is event time when it ends like a time (_at, _time, _ts, _on, _date).
func temporalRole(f *Field) timeRole {
	if t, ok := f.Annotations.Get("time"); ok {
		return annotatedRole(t)
	}
	return nameRole(splitWords(f.Name))
}

// annotatedRole maps a time= value to a role; business time is event time
// and system time is processing time.
func annotatedRole(t string) timeRole {
	switch t {
	case "event", "business":
		return roleEvent
	case "ingested":
		return roleIngested
	case "processed", "system":
		return roleProcessed
	default:
		return roleNone
	}
}

// nameRole classifies a field name's words.
func nameRole(ws []string) timeRole {
	found := roleNone
	for _, w := range ws {
		switch r := wordRole(w); {
		case r == roleNone:
		case found != roleNone && found != r:
			return roleConflict
		default:
			found = r
		}
	}
	if found == roleNone && len(ws) > 0 && timeStem(ws[len(ws)-1]) {
		return roleEvent
	}
	return found
}

// wordRole is the role one word of a field name signals, from the §9.1-A
// lexicon.
func wordRole(w string) timeRole {
	switch w {
	case "ingest", "ingested", "loaded", "received", "arrived", "landed", "synced":
		return roleIngested
	case "processed", "transformed", "computed", "etl", "batch", "run", "updated", "modified",
		"deleted":
		return roleProcessed
	case "created", "inserted":
		return roleAmbiguous
	case "occurred", "event", "happened", "captured", "placed", "ordered", "clicked":
		return roleEvent
	default:
		return roleNone
	}
}

func timeStem(w string) bool {
	switch w {
	case "at", "time", "ts", "timestamp", "on", "date":
		return true
	default:
		return false
	}
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
