package mixedmodellint

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
)

// Ways a column's time unit is known.
const (
	heldByType heldSource = iota + 1
	heldByWrites
	heldByGrain
)

// scope resolves a query's relation aliases to entities. Derived relations
// (CTEs, subqueries) and names with no entity resolve to nil, and designs
// skip them rather than guess.
type scope struct {
	q        *Query
	entities map[string]*Entity // by lowercase alias
}

// heldSource is how the time unit a column holds is known.
type heldSource int

// held is the finest time unit a column can hold, how that is known, and,
// for truncating writes, where one is.
type held struct {
	unit string
	from heldSource
	at   Position
}

// ancestorCopy is an origin of a written value that is a field of an
// ancestor of the entity written.
type ancestorCopy struct {
	parent *Entity
	field  string
	at     Position
}

// section7QueryDesigns are the §7 designs that read the query corpus.
func section7QueryDesigns() []design {
	return []design{
		{
			id:        "7.1-C",
			title:     "Grain inferred from writes",
			strength:  Must,
			heuristic: true,
			check:     check71C,
		},
		{id: "7.2-A", title: "Static fan-out analysis", strength: Must, check: check72A},
		{id: "7.3-A", title: "Write-statement idempotency", strength: Must, check: check73A},
		{id: "7.3-D", title: "Readers deduplicating at the grain", strength: Must, check: check73D},
		{
			id: "7.4-D", title: "Copy-on-write from the coarser grain",
			strength: Should, check: check74D,
		},
		{
			id: "7.5-D", title: "Query asks for finer detail than the source holds",
			strength: Should, check: check75D,
		},
	}
}

func (c *catalog) scopeOf(q *Query) scope {
	s := scope{q: q, entities: map[string]*Entity{}}
	for _, r := range q.Relations {
		if r.Derived {
			continue
		}
		if e := c.lookup(r.Name, SourceSQL); e != nil {
			s.entities[strings.ToLower(r.Alias)] = e
		}
	}
	return s
}

func (s scope) entity(alias string) *Entity {
	return s.entities[strings.ToLower(alias)]
}

// resolve returns the stored entity and field a column reference names: by
// its qualifier, or by the query's only relation when it has none. Either
// is nil when the reference is to a derived relation, an entity outside the
// catalog, or a column the entity does not have.
func (s scope) resolve(ref ColumnRef) (*Entity, *Field) {
	alias := cmp.Or(ref.Relation, defaultAlias(s.q))
	e := s.entity(alias)
	if e == nil {
		return nil, nil
	}
	f, _ := e.Field(ref.Column)
	return e, f
}

// grainKey returns the columns that identify one row of e: its declared
// grain's columns, or else its primary key.
func (c *catalog) grainKey(e *Entity) []string {
	if g, _, ok := c.groupGrain(e); ok && len(g.columns) > 0 && len(g.unresolved) == 0 {
		return g.columns
	}
	return e.PrimaryKey
}

// coversKey reports whether cols include every column of some key of e.
func coversKey(e *Entity, cols []string) bool {
	for _, k := range e.Keys() {
		if len(k) > 0 && subsetFold(k, cols) {
			return true
		}
	}
	return false
}

func subsetFold(sub, set []string) bool {
	for _, s := range sub {
		if !containsFold(set, s) {
			return false
		}
	}
	return true
}

func check71C(c *catalog) []issue {
	var out []issue
	for i := range c.queries {
		q := &c.queries[i]
		if (q.Kind != QueryInsert && q.Kind != QueryMerge) || len(q.ConflictKey) == 0 {
			continue
		}
		e := c.lookup(q.Target, SourceSQL)
		if e == nil {
			continue
		}
		g, _, ok := c.groupGrain(e)
		if !ok || len(g.columns) == 0 || sameColumns(g.columns, q.ConflictKey) {
			continue
		}
		out = append(out, issue{
			entity: e, pos: q.Pos,
			measured: fmt.Sprintf("a writer upserts %s on (%s), but its grain %q implies (%s)",
				e.Name, strings.Join(q.ConflictKey, ", "), g.text, strings.Join(g.columns, ", ")),
			suggestion: fmt.Sprintf("Reconcile them: either the grain is one row per (%s) and "+
				"the grain statement should say so, or the upsert should be ON CONFLICT (%s).",
				strings.Join(q.ConflictKey, ", "), strings.Join(g.columns, ", ")),
		})
	}
	return out
}

// check72A reports SUM, AVG and COUNT(col) over a relation whose rows a join
// multiplies: a join on a column set that is not unique on the joined side
// raises the grain to that side's rows (rule_plan.md §7.2-A). COUNT(*) counts
// the result's own rows and COUNT(DISTINCT), MIN and MAX are immune, so none
// of them is reported.
func check72A(c *catalog) []issue {
	var out []issue
	for i := range c.queries {
		q := &c.queries[i]
		if len(q.Joins) == 0 {
			continue
		}
		s := c.scopeOf(q)
		for j := range q.Aggregates {
			a := &q.Aggregates[j]
			if !fanOutSensitive(a) {
				continue
			}
			if is, ok := fanOut(c, s, a); ok {
				out = append(out, is)
			}
		}
	}
	return out
}

func fanOutSensitive(a *Aggregate) bool {
	if a.Distinct || a.Star || a.Arg.Column == "" {
		return false
	}
	return a.Func == "sum" || a.Func == "avg" || a.Func == "count"
}

// fanOut finds the first join that multiplies the rows of the aggregate's
// relation: a join to another relation on columns that are not a key of it,
// or a join of the aggregate's own relation on columns that are not a key of
// the other side.
func fanOut(c *catalog, s scope, a *Aggregate) (issue, bool) {
	from := s.entity(a.Arg.Relation)
	if from == nil {
		return issue{}, false
	}
	for _, j := range s.q.Joins {
		child := multiplier(s, j, a.Arg.Relation)
		if child == nil {
			continue
		}
		key := strings.Join(c.grainKey(from), ", ")
		return issue{
			entity: from,
			field:  a.Arg.Column,
			pos:    a.Pos,
			measured: fmt.Sprintf("%s(%s.%s) is computed after a join to %s that raises the grain "+
				"to one row per %s, so each %s's %s is counted once per matching %s",
				strings.ToUpper(a.Func), from.Name, a.Arg.Column, child.Name, noun(child),
				noun(from), a.Arg.Column, noun(child)),
			suggestion: fmt.Sprintf("Compute %s(%s.%s) without the join to %s, or aggregate %s "+
				"in a CTE grouped by the join key first and join that one row per %s (§7.2). "+
				"The grain key of %s is (%s).", strings.ToUpper(a.Func), from.Name, a.Arg.Column,
				child.Name, child.Name, noun(from), from.Name, key),
		}, true
	}
	return issue{}, false
}

// noun names one row of e in prose: order_lines is "order line".
func noun(e *Entity) string {
	return strings.ReplaceAll(Canonical(e.Name), "_", " ")
}

// multiplier returns the entity whose rows join j repeats each row of the
// relation aliased rel by, or nil when j does not multiply them: j joins
// another relation on columns that are not one of its keys, or j joins rel
// itself on columns that are not a key of the other side.
func multiplier(s scope, j Join, rel string) *Entity {
	right := s.entity(j.Right)
	if right == nil || len(j.On) == 0 {
		return nil
	}
	rightCols, leftAlias, leftCols := joinSides(j)
	left := s.entity(leftAlias)
	switch {
	case !strings.EqualFold(j.Right, rel) && !coversKey(right, rightCols):
		return right
	case strings.EqualFold(j.Right, rel) && left != nil && !coversKey(left, leftCols):
		return left
	default:
		return nil
	}
}

// joinSides returns the joined relation's columns, the other side's
// relation, and the other side's columns. When the other side's columns name
// more than one relation, the first is used.
func joinSides(j Join) (rightCols []string, leftAlias string, leftCols []string) {
	for _, p := range j.On {
		rightCols = append(rightCols, p.Right.Column)
		if leftAlias == "" {
			leftAlias = p.Left.Relation
		}
		if strings.EqualFold(p.Left.Relation, leftAlias) {
			leftCols = append(leftCols, p.Left.Column)
		}
	}
	return rightCols, leftAlias, leftCols
}

// check73A reports plain INSERTs into an incrementally loaded entity: a
// retry either duplicates rows or fails, so the load is not idempotent. A
// write with ON CONFLICT or MERGE is idempotent; whether its key matches the
// grain is 7.1-C's question. An entity keyed by an event ID rejects
// duplicates, which is the accepted append-only pattern.
func check73A(c *catalog) []issue {
	var out []issue
	for i := range c.queries {
		q := &c.queries[i]
		if q.Kind != QueryInsert || q.ConflictKey != nil {
			continue
		}
		e := c.lookup(q.Target, SourceSQL)
		if e == nil || !loadedIncrementally(e) || hasEventKey(e.Keys()) {
			continue
		}
		key := strings.Join(c.grainKey(e), ", ")
		if key == "" {
			key = "<grain columns>"
		}
		out = append(out, issue{
			entity: e,
			pos:    q.Pos,
			measured: fmt.Sprintf(
				"a plain INSERT appends to %s, which is loaded incrementally, so a "+
					"retried load duplicates rows or fails",
				e.Name,
			),
			suggestion: fmt.Sprintf("Use INSERT ... ON CONFLICT (%s) DO UPDATE, or MERGE ... ON "+
				"(%s), or replace the load's partition, so running it twice has the effect of "+
				"running it once (§7.3).", key, key),
		})
	}
	return out
}

func check73D(c *catalog) []issue {
	var out []issue
	for i := range c.queries {
		q := &c.queries[i]
		s := c.scopeOf(q)
		for j := range q.Dedups {
			if is, ok := dedupAtGrain(c, s, &q.Dedups[j]); ok {
				out = append(out, is)
			}
		}
	}
	return out
}

// dedupAtGrain reports a dedup whose partition covers the grain key of the
// entity it reads, which means readers expect duplicates the grain forbids.
// A latest-version read of a temporal entity (a window ordered descending by
// a validity or transaction time) and the one dedup in the model that builds
// the next layer from a staging entity are expected and not reported.
func dedupAtGrain(c *catalog, s scope, d *Dedup) (issue, bool) {
	if len(d.Partition) == 0 {
		return issue{}, false
	}
	e := s.entity(d.Partition[0].Relation)
	if e == nil {
		return issue{}, false
	}
	var cols []string
	for _, p := range d.Partition {
		if strings.EqualFold(p.Relation, d.Partition[0].Relation) {
			cols = append(cols, p.Column)
		}
	}
	key := c.grainKey(e)
	if len(key) == 0 || !subsetFold(key, cols) || latestVersionRead(e, d) ||
		stagingBoundary(e, s.q) {
		return issue{}, false
	}
	g, _, _ := c.groupGrain(e)
	grain := quotedOr(g.text, "its key ("+strings.Join(key, ", ")+")")
	return issue{
		entity: e, pos: d.Pos,
		measured: fmt.Sprintf("a query deduplicates %s on (%s), which is its grain key, so %s "+
			"holds duplicates at its stated grain %s", e.Name, strings.Join(cols, ", "), e.Name, grain),
		suggestion: fmt.Sprintf("Make the load idempotent on (%s) (§7.3-A) and add UNIQUE (%s), "+
			"then drop the dedup. If several rows per (%s) are expected, the grain is finer: "+
			"correct the grain statement (§7.1).", strings.Join(key, ", "), strings.Join(key, ", "),
			strings.Join(key, ", ")),
	}, true
}

func latestVersionRead(e *Entity, d *Dedup) bool {
	if d.Kind != DedupWindow || !d.Descending || len(d.OrderBy) == 0 {
		return false
	}
	f, ok := e.Field(d.OrderBy[0].Column)
	if !ok {
		return false
	}
	t, _ := f.Annotations.Get("time")
	name := strings.ToLower(f.Name)
	return t == "valid_from" || t == "txn_from" || name == "valid_from" ||
		strings.HasSuffix(name, "_version") || name == "version"
}

func stagingBoundary(e *Entity, q *Query) bool {
	layer, _ := e.Annotations.Get("layer")
	return layer == "staging" && q.Target != ""
}

// quotedOr returns a quoted, or b when a is empty.
func quotedOr(a, b string) string {
	if a != "" {
		return fmt.Sprintf("%q", a)
	}
	return b
}

// check74BQuery is the query half of 7.4-B: an aggregate over a table that
// holds several grains must filter on the grain_level discriminator, or it
// adds daily rows to monthly totals.
func check74BQuery(c *catalog) []issue {
	var out []issue
	for i := range c.queries {
		q := &c.queries[i]
		s := c.scopeOf(q)
		for _, a := range q.Aggregates {
			e := s.entity(cmp.Or(a.Arg.Relation, defaultAlias(q)))
			if e == nil {
				continue
			}
			disc, ok := discriminator(c, e)
			if !ok || filtersOn(q, disc.Name) {
				continue
			}
			out = append(out, issue{
				entity: e,
				field:  disc.Name,
				pos:    a.Pos,
				measured: fmt.Sprintf("%s over %s, which mixes grains, does not filter on %s",
					strings.ToUpper(a.Func), e.Name, disc.Name),
				suggestion: fmt.Sprintf(
					"Add WHERE %s = '<level>' so the aggregate reads one grain.",
					disc.Name,
				),
			})
			break
		}
	}
	return out
}

func filtersOn(q *Query, col string) bool {
	return slices.ContainsFunc(
		q.Filters,
		func(f ColumnRef) bool { return strings.EqualFold(f.Column, col) },
	)
}

func defaultAlias(q *Query) string {
	if len(q.Relations) == 1 {
		return q.Relations[0].Alias
	}
	return ""
}

func check75D(c *catalog) []issue {
	var out []issue
	for i := range c.queries {
		q := &c.queries[i]
		s := c.scopeOf(q)
		for j := range q.TimeExprs {
			if is, ok := finerThanGrain(c, s, &q.TimeExprs[j]); ok {
				out = append(out, is)
			}
		}
	}
	return out
}

// finerThanGrain reports a time expression that asks for a finer unit than
// its column can hold. A date column holds days, so date_trunc('hour', d) is
// midnight for every row; a timestamp that every write truncates to the
// minute (§9.8-B) holds minutes. A grain works in one direction (rule_plan.md
// §7.5-D).
func finerThanGrain(c *catalog, s scope, te *TimeExpr) (issue, bool) {
	e := s.entity(te.Column.Relation)
	if e == nil {
		return issue{}, false
	}
	f, ok := e.Field(te.Column.Column)
	if !ok {
		return issue{}, false
	}
	h, ok := c.heldUnit(e, f)
	if !ok {
		return issue{}, false
	}
	need := timeUnitRank(te.Unit)
	if need == 0 || need >= timeUnitRank(h.unit) {
		return issue{}, false
	}
	asked := te.Unit
	if te.Func == "compare" {
		asked = "a time of day"
	}
	is := issue{entity: e, field: f.Name, pos: te.Pos}
	switch h.from {
	case heldByType:
		is.measured = fmt.Sprintf("a query asks for %s from %s.%s, a date column whose finest "+
			"unit is a day, so every value falls on midnight and the result looks valid while "+
			"saying nothing", asked, e.Name, f.Name)
		is.suggestion = finerSourceHint(c, e)
	case heldByWrites:
		is.measured = fmt.Sprintf("a query asks for %s from %s.%s, but every write truncates it "+
			"to the %s (as at %s), so every value within a %s is the same and the result looks "+
			"valid while saying nothing", asked, e.Name, f.Name, h.unit, h.at, h.unit)
		is.suggestion = fmt.Sprintf("Store %s.%s at full precision and truncate when reading "+
			"(§9.8-B), or ask for the %s or coarser.", e.Name, f.Name, h.unit)
	default:
		is.measured = fmt.Sprintf("a query asks for %s from %s.%s, but %s's declared grain holds "+
			"one row per %s, so nothing finer is there to find", asked, e.Name, f.Name, e.Name,
			h.unit)
		is.suggestion = finerSourceHint(c, e)
	}
	return is, true
}

// heldUnit returns the finest time unit f can hold, and why: a day for a
// date; for a timestamp, the unit every write truncates it to (§9.8-B), or
// else the time unit of the declared grain when f is the grain's time
// column. ok is false when f holds full precision or is not temporal.
func (c *catalog) heldUnit(e *Entity, f *Field) (held, bool) {
	switch f.Type {
	case TypeDate:
		return held{unit: "day", from: heldByType}, true
	case TypeTimestamptz, TypeTimestampNaive:
		if unit, at, ok := c.writtenUnit(e, f.Name); ok {
			return held{unit: unit, at: at, from: heldByWrites}, true
		}
		if unit := grainUnit(e, f); unit != "" {
			return held{unit: unit, from: heldByGrain}, true
		}
	default:
	}
	return held{}, false
}

// grainUnit returns the time unit e's declared grain names for f ("one row
// per sensor per hour" for the key's timestamp), or "".
func grainUnit(e *Entity, f *Field) string {
	g, ok := grainOf(e)
	if !ok {
		return ""
	}
	for _, n := range g.nouns {
		if timeUnitRank(n) > 0 && containsFold(nounColumns(e, n), f.Name) {
			return n
		}
	}
	return ""
}

// finerSourceHint names a source with timestamps that e is built from, when
// the query corpus defines e as a view over one.
func finerSourceHint(c *catalog, e *Entity) string {
	for i := range c.queries {
		q := &c.queries[i]
		if q.Kind != QueryCreateView || !strings.EqualFold(q.Target, e.Name) {
			continue
		}
		for _, r := range q.Relations {
			src := c.lookup(r.Name, SourceSQL)
			if src != nil && !r.Derived && hasTimestamp(src) {
				return fmt.Sprintf("Read %s instead, which %s is built from and which records "+
					"timestamps.", src.Name, e.Name)
			}
		}
	}
	return fmt.Sprintf("No source in the repository holds finer-than-day data for %s. Store it at "+
		"a finer grain (§7.5), or declare @recoverable_from: <upstream> if the upstream system "+
		"still has it.", e.Name)
}

func hasTimestamp(e *Entity) bool {
	return slices.ContainsFunc(e.Fields, func(f Field) bool {
		return f.Type == TypeTimestamptz || f.Type == TypeTimestampNaive
	})
}

func check74D(c *catalog) []issue {
	var out []issue
	seen := map[string]bool{}
	for _, w := range c.writes() {
		if w.field == nil || ownColumn(c, w.entity, w.field) {
			continue
		}
		for _, cp := range c.ancestorCopies(w) {
			key := fmt.Sprintf("%s|%s|%s|%s", cp.at, w.entity.ID, w.field.Name, cp.parent.ID)
			if !seen[key] {
				seen[key] = true
				out = append(out, copiedFromAncestor(w, cp))
			}
		}
	}
	return out
}

// ancestorCopies returns the origins of w's value that copy a field of an
// entity w's entity references, directly or through others.
func (c *catalog) ancestorCopies(w write) []ancestorCopy {
	var out []ancestorCopy
	ancestors := c.ancestors(w.entity)
	for i := range w.assignment.Origins {
		o := &w.assignment.Origins[i]
		if o.From.Entity == "" {
			continue
		}
		if p := c.lookup(o.From.Entity, w.entity.Source); slices.Contains(ancestors, p) {
			out = append(out, ancestorCopy{parent: p, field: o.From.Field, at: w.at(o)})
		}
	}
	return out
}

// ownColumn reports whether a child may legitimately hold a value copied
// from an ancestor: it is a key, a reference or part of the grain key, it is
// declared denormalized_for=, or it is a snapshot taken when the transaction
// happened (§6.6), such as a line's unit price.
func ownColumn(c *catalog, e *Entity, f *Field) bool {
	return isKeyOrRef(e, f.Name) || containsFold(c.grainKey(e), f.Name) ||
		f.Annotations.Has("denormalized_for") || f.Annotations.Has("captured_at_transaction")
}

func copiedFromAncestor(w write, cp ancestorCopy) issue {
	child, field, parent, from := w.entity.Name, w.field.Name, cp.parent, cp.field
	return issue{
		entity: w.entity, field: field, pos: cp.at,
		measured: fmt.Sprintf("a write copies %s.%s into every %s row as %s, so the value "+
			"repeats per row and is multiplied when summed", parent.Name, from, child, field),
		suggestion: fmt.Sprintf("Read %s from %s with a join instead, or declare "+
			"denormalized_for=<reason> on %s.%s and mark it measure=non_additive (§7.4).",
			from, parent.Name, child, field),
	}
}
