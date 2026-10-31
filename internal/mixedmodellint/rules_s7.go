package mixedmodellint

import (
	"fmt"
	"strings"
)

// section7Designs are the grain designs of rule_plan.md §7 that read only the
// IR. The query-corpus designs are in rules_s7_query.go.
func section7Designs() []design {
	return []design{
		{id: "7.1-A", title: "Grain-statement contract", strength: Must, check: check71A},
		{id: "7.1-B", title: "Grain and key consistency", strength: Must, check: check71B},
		{id: "7.3-B", title: "Dedup-key structure", strength: Must, check: check73B},
		{
			id:       "7.4-B",
			title:    "Mixed-grain discriminator contract",
			strength: Should,
			check:    check74B,
		},
		// rule_plan.md: same-name-and-type alone reports at warning (M5), so
		// it is not demoted as a heuristic.
		{
			id: "7.4-C", title: "Functional dependency across linked entities",
			strength: Should, check: check74C,
		},
		{
			id: "7.5-C", title: "Pre-aggregated ingestion", strength: Should,
			heuristic: true, check: check75C,
		},
	}
}

// grainExempt reports whether §7.1 does not apply to e: scratch work (§1.7)
// and embedded value objects, which have no rows of their own.
func grainExempt(e *Entity) bool {
	lc, _ := e.Annotations.Get("lifecycle")
	return lc == "scratch" || e.Annotations.Has("value-object")
}

// groupGrain returns the grain declared on e or, failing that, on any entity
// linked to it, so a table and its Go struct need not both carry it.
func (c *catalog) groupGrain(e *Entity) (grainInfo, *Entity, bool) {
	if g, ok := grainOf(e); ok {
		return g, e, true
	}
	for _, other := range c.byCanonical[e.Canonical] {
		if g, ok := grainOf(other); ok {
			return g, other, true
		}
	}
	return grainInfo{}, nil, false
}

func check71A(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		if grainExempt(e) {
			continue
		}
		g, owner, ok := c.groupGrain(e)
		switch {
		case !ok:
			out = append(out, missingGrain(e))
		case owner != e:
			continue // checked on the entity that declares it
		case !c.rx.grainPattern.MatchString(g.text):
			out = append(out, issue{
				entity: e, pos: grainPos(e),
				measured: fmt.Sprintf("grain %q does not have the form \"one row per X\"", g.text),
				suggestion: fmt.Sprintf("State the grain as one sentence naming what a row "+
					"is, for example %s.", annotationHint(e, "grain", grainOrPlaceholder(e))),
			})
		case len(g.unresolved) > 0:
			out = append(out, issue{
				entity: e, pos: grainPos(e),
				measured: fmt.Sprintf("grain %q names %s, which is not a field, a referenced "+
					"entity or a time unit of %s", g.text, quoteList(g.unresolved), e.Name),
				suggestion: "Name the grain with the entity or column that identifies a row, " +
					"or add the missing column.",
			})
		case g.fp != "" && g.fp != keyFingerprint(e):
			out = append(out, issue{
				entity: e, pos: grainPos(e),
				measured: fmt.Sprintf("grain declared at fp=%s, but the key structure is now "+
					"fp=%s (mml/stale-declaration)", g.fp, keyFingerprint(e)),
				suggestion: fmt.Sprintf("The key of %s changed since this grain was written. "+
					"Confirm the grain still holds and update it to fp=%s.", e.Name, keyFingerprint(e)),
			})
		}
	}
	return out
}

func missingGrain(e *Entity) issue {
	inferred := inferGrain(e)
	hint := grainOrPlaceholder(e) + " fp=" + keyFingerprint(e)
	measured := e.Name + " has no grain statement"
	if inferred != "" {
		measured += fmt.Sprintf(" (its primary key implies %q)", inferred)
	}
	return issue{
		entity:   e,
		measured: measured,
		suggestion: fmt.Sprintf("Declare what one row of %s represents: %s.",
			e.Name, annotationHint(e, "grain", hint)),
	}
}

func grainOrPlaceholder(e *Entity) string {
	if g := inferGrain(e); g != "" {
		return g
	}
	return "one row per <thing>"
}

func check71B(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		g, owner, ok := c.groupGrain(e)
		if !ok || grainExempt(e) || len(g.unresolved) > 0 || len(g.columns) == 0 {
			continue
		}
		keys := e.Keys()
		if len(keys) == 0 {
			if owner == e && e.Kind == KindTable {
				out = append(out, issue{
					entity: e, pos: grainPos(e),
					measured: fmt.Sprintf("grain %q is unverifiable: %s has no primary or "+
						"unique key", g.text, e.Name),
					suggestion: fmt.Sprintf("Add PRIMARY KEY (%s), so the database rejects a "+
						"second row at the grain (§7.3).", strings.Join(g.columns, ", ")),
				})
			}
			continue
		}
		if !hasKey(keys, g.columns) {
			out = append(out, issue{
				entity: e, pos: grainPos(e),
				measured: fmt.Sprintf("grain %q implies key (%s), but %s has %s", g.text,
					strings.Join(g.columns, ", "), e.Name, describeKeys(e)),
				suggestion: fmt.Sprintf("Correct the grain statement to match the key (the key "+
					"implies %q); or, if %q is what a row means, add UNIQUE (%s).",
					inferGrain(e), g.text, strings.Join(g.columns, ", ")),
			})
		}
	}
	return out
}

func hasKey(keys [][]string, cols []string) bool {
	for _, k := range keys {
		if sameColumns(k, cols) {
			return true
		}
	}
	return false
}

func describeKeys(e *Entity) string {
	var parts []string
	if len(e.PrimaryKey) > 0 {
		parts = append(parts, "primary key ("+strings.Join(e.PrimaryKey, ", ")+")")
	}
	for _, u := range e.Uniques {
		parts = append(parts, "unique ("+strings.Join(u, ", ")+")")
	}
	return strings.Join(parts, " and ")
}

func check73B(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		if !loadedIncrementally(e) || e.Kind != KindTable {
			continue
		}
		cols, ok := missingDedupKey(c, e)
		if !ok {
			continue
		}
		out = append(out, issue{
			entity: e,
			measured: fmt.Sprintf("%s is loaded incrementally, but no key rejects duplicates "+
				"at its grain (%s)", e.Name, cols),
			suggestion: fmt.Sprintf("Add UNIQUE (%s), or an idempotency_key column with a "+
				"unique constraint, so a retried load cannot insert the same row twice.", cols),
		})
	}
	return out
}

// missingDedupKey returns the grain columns an incrementally loaded entity
// needs a key on, and true when no key covers them. An entity with no grain
// but some key passes here, since 7.1-A already reports the missing grain.
func missingDedupKey(c *catalog, e *Entity) (string, bool) {
	keys := e.Keys()
	if hasEventKey(keys) {
		return "", false
	}
	g, _, hasGrain := c.groupGrain(e)
	if !hasGrain || len(g.columns) == 0 {
		return "event_id", len(keys) == 0
	}
	return strings.Join(g.columns, ", "), !hasKey(keys, g.columns)
}

// loadedIncrementally reports whether e is appended to rather than rebuilt:
// load=incremental, or an event, analytical or feature layer without
// load=full.
func loadedIncrementally(e *Entity) bool {
	load, _ := e.Annotations.Get("load")
	if load == "full" {
		return false
	}
	if load == "incremental" {
		return true
	}
	layer, _ := e.Annotations.Get("layer")
	return layer == "event" || layer == "analytical" || layer == "feature"
}

func hasEventKey(keys [][]string) bool {
	for _, k := range keys {
		if len(k) == 1 {
			c := strings.ToLower(k[0])
			if c == "event_id" || c == "idempotency_key" || strings.HasSuffix(c, "_event_id") {
				return true
			}
		}
	}
	return false
}

func check74B(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		g, _ := grainOf(e)
		disc, hasDisc := discriminator(c, e)
		mixed := strings.Contains(" "+strings.ToLower(g.text)+" ", " or ") || hasDisc
		if !mixed {
			continue
		}
		switch {
		case !hasDisc:
			out = append(out, issue{
				entity: e, pos: grainPos(e),
				measured: fmt.Sprintf("grain %q mixes levels, and %s has no grain_level column",
					g.text, e.Name),
				suggestion: fmt.Sprintf("Add grain_level TEXT NOT NULL CHECK (grain_level IN "+
					"(...)) naming each level, and filter on it in every query; better still, "+
					"split %s into one table per grain.", e.Name),
			})
		case disc.Nullable || !hasEnum(e, disc):
			out = append(out, issue{
				entity: e, field: disc.Name, pos: disc.Pos,
				measured: fmt.Sprintf("%s.%s marks mixed grains but is %s", e.Name, disc.Name,
					discriminatorProblem(e, disc)),
				suggestion: fmt.Sprintf("Make %s NOT NULL with CHECK (%s IN (...)) listing each "+
					"level, so no row has an unknown grain.", disc.Name, disc.Name),
			})
		}
	}
	return out
}

func discriminator(c *catalog, e *Entity) (*Field, bool) {
	for i := range e.Fields {
		f := &e.Fields[i]
		if c.rx.mixedField.MatchString(f.Name) {
			return f, true
		}
	}
	return nil, false
}

func hasEnum(e *Entity, f *Field) bool {
	if len(f.Enum) > 0 {
		return true
	}
	for _, ch := range e.Checks {
		if strings.EqualFold(ch.Field, f.Name) && len(ch.Enum) > 0 {
			return true
		}
	}
	return false
}

func discriminatorProblem(e *Entity, f *Field) string {
	switch {
	case f.Nullable && !hasEnum(e, f):
		return "nullable and unconstrained"
	case f.Nullable:
		return "nullable"
	default:
		return "not constrained to a list of levels"
	}
}

// fdExcluded are names a child legitimately shares with its parent: system
// and audit columns, and generic attributes such as a line's own status.
func fdExcluded(f *Field) bool {
	if t, _ := f.Annotations.Get("time"); t == "system" {
		return true
	}
	switch strings.ToLower(f.Name) {
	case "id", "created_at", "updated_at", "deleted_at", "_loaded_at", "name", "status",
		"description", "version":
		return true
	default:
		return false
	}
}

func check74C(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		for _, p := range c.parents(e) {
			out = append(out, dependentFields(e, p)...)
		}
	}
	return out
}

// dependentFields reports fields of child that repeat a field of parent with
// the same name and type, other than keys, references and excluded names.
func dependentFields(child, parent *Entity) []issue {
	var out []issue
	for i := range child.Fields {
		f := &child.Fields[i]
		if fdExcluded(f) || f.Annotations.Has("denormalized_for") || isKeyOrRef(child, f.Name) {
			continue
		}
		pf, ok := parent.Field(f.Name)
		if !ok || pf.Type != f.Type || f.Type == TypeUnknown {
			continue
		}
		out = append(out, issue{
			entity: child, field: f.Name, pos: f.Pos,
			measured: fmt.Sprintf("%s.%s repeats %s.%s (same name and type), so it depends on "+
				"the reference to %s rather than on %s's grain", child.Name, f.Name, parent.Name,
				pf.Name, parent.Name, child.Name),
			suggestion: fmt.Sprintf("Read %s from %s with a join instead of storing it on every "+
				"%s row, or declare denormalized_for=<reason> on it (§3.2).", f.Name,
				parent.Name, child.Name),
		})
	}
	return out
}

func isKeyOrRef(e *Entity, name string) bool {
	for _, k := range e.Keys() {
		if containsFold(k, name) {
			return true
		}
	}
	for _, r := range e.Refs {
		if containsFold(r.Fields, name) {
			return true
		}
	}
	return false
}

func containsFold(list []string, s string) bool {
	for _, v := range list {
		if strings.EqualFold(v, s) {
			return true
		}
	}
	return false
}

func check75C(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		layer, _ := e.Annotations.Get("layer")
		src, _ := e.Annotations.Get("source")
		isEvent := layer == "event" || strings.HasSuffix(strings.ToLower(e.Name), "_events")
		if !isEvent || src == "third_party" {
			continue
		}
		if reason, ok := preAggregated(c, e); ok {
			out = append(out, issue{
				entity:   e,
				measured: fmt.Sprintf("event entity %s %s", e.Name, reason),
				suggestion: fmt.Sprintf("Store one row per event in %s, with event_id and an "+
					"event_time timestamptz at source precision (§9.8), and derive counts "+
					"from it.", e.Name),
			})
		}
	}
	return out
}

// preAggregated says why an event entity looks aggregated at ingest: its only
// time column is a date, or it has a counter column and no event key.
func preAggregated(c *catalog, e *Entity) (string, bool) {
	dates, stamps := timeColumnCounts(e)
	if dates > 0 && stamps == 0 {
		return "records time only as a date, so it holds daily totals, not events", true
	}
	if hasEventKey(e.Keys()) {
		return "", false
	}
	for i := range e.Fields {
		f := &e.Fields[i]
		if (f.Type == TypeInt || f.Type == TypeDecimal) && c.rx.aggCounter.MatchString(f.Name) {
			return fmt.Sprintf("has counter column %s and no event_id key", f.Name), true
		}
	}
	return "", false
}

// timeColumnCounts counts e's date columns and its timestamp columns.
func timeColumnCounts(e *Entity) (dates, stamps int) {
	for i := range e.Fields {
		switch e.Fields[i].Type {
		case TypeDate:
			dates++
		case TypeTimestamptz, TypeTimestampNaive:
			stamps++
		default:
		}
	}
	return dates, stamps
}

// grainPos returns where e's grain was declared, or e's position.
func grainPos(e *Entity) Position {
	if a, ok := e.Annotations["grain"]; ok && a.Pos != (Position{}) {
		return a.Pos
	}
	return e.Pos
}

// annotationHint writes key=value in the syntax of e's source, for a
// suggestion the user can paste.
func annotationHint(e *Entity, key, value string) string {
	switch e.Source {
	case SourceGo:
		return fmt.Sprintf("//mml:%s %s", key, value)
	case SourceJSONSchema:
		return fmt.Sprintf(`"x-mml-%s": %q`, key, value)
	default:
		return fmt.Sprintf("COMMENT ON TABLE %s IS '@%s: %s'", e.Name, key, value)
	}
}

func quoteList(items []string) string {
	q := make([]string, len(items))
	for i, s := range items {
		q[i] = fmt.Sprintf("%q", s)
	}
	return strings.Join(q, ", ")
}
