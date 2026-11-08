package mixedmodellint

import (
	"fmt"
	"slices"
	"strings"
)

// junctionAttributes is how many attributes of its own make a junction a
// relationship entity that deserves a key and a name (§6.4-B).
const junctionAttributes = 2

// satelliteGroup is a single-column reference and the fields beside it that
// describe the relationship rather than the row: spouse_id with
// spouse_since.
type satelliteGroup struct {
	ref        *Ref
	stem       string
	target     *Entity
	satellites []*Field
}

func section6PromoteDesigns() []design {
	return []design{
		{
			id: "6.4-A", title: "Relationship-attribute satellites", strength: Should,
			heuristic: true, check: check64A,
		},
		{
			id: "6.4-B", title: "Fat junction without its own key", strength: Should,
			check: check64B,
		},
		{
			id: "6.4-C", title: "Temporal-relationship growth since the lock", strength: Should,
			check: check64C,
		},
	}
}

// refStem returns the name a single-column reference is about: spouse for
// spouse_id or SpouseID, or "" for a composite reference.
func refStem(r *Ref) string {
	if len(r.Fields) != 1 {
		return ""
	}
	return strings.TrimSuffix(snakeName(r.Fields[0]), "_id")
}

// satelliteGroups returns e's references that have satellite fields.
func (c *catalog) satelliteGroups(e *Entity) []satelliteGroup {
	var out []satelliteGroup
	for i := range e.Refs {
		r := &e.Refs[i]
		stem := refStem(r)
		if stem == "" {
			continue
		}
		g := satelliteGroup{ref: r, stem: stem, target: c.lookup(r.Target, e.Source)}
		for j := range e.Fields {
			suffix, ok := strings.CutPrefix(snakeName(e.Fields[j].Name), stem+"_")
			if ok && c.rx.satellite[suffix] {
				g.satellites = append(g.satellites, &e.Fields[j])
			}
		}
		if len(g.satellites) > 0 {
			out = append(out, g)
		}
	}
	return out
}

// check64A reports a reference whose relationship carries data of its own
// on the referencing row (rule_plan.md §6.4-A): spouse_id beside
// spouse_since or spouse_role. The names alone make a note; it is a warning
// when the satellites' types fit their suffixes (dates for _since, text for
// _role) and the reference is to the entity itself, a peer, so the data
// belongs to the pair (M5). A reference to a master entity is left a note:
// product_since usually describes the row. Peers other than the entity
// itself cannot be told apart from masters yet.
func check64A(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		if outsideConventions(e) {
			continue
		}
		for _, g := range c.satelliteGroups(e) {
			out = append(out, c.satelliteIssue(e, &g))
		}
	}
	return out
}

func (c *catalog) satelliteIssue(e *Entity, g *satelliteGroup) issue {
	names := make([]string, len(g.satellites))
	for i, f := range g.satellites {
		names[i] = f.Name
	}
	self := g.target == e
	rel, other := snakeName(e.Canonical)+"_"+g.stem, g.stem
	sides := fmt.Sprintf("%s_id, %s_id", snakeName(e.Canonical), other)
	if self {
		sides = fmt.Sprintf("%s_a_id, %s_b_id", snakeName(e.Canonical), snakeName(e.Canonical))
	}
	is := issue{
		entity: e, field: g.ref.Fields[0], pos: g.satellites[0].Pos,
		measured: fmt.Sprintf("%s.%s carries relationship data (%s)", e.Name, g.ref.Fields[0],
			strings.Join(names, ", ")),
		suggestion: fmt.Sprintf("Promote the relationship to an entity, %s(%s_id, %s, %s), "+
			"and drop %s and its satellites from %s (§6.4).", rel, rel, sides,
			strings.Join(names, ", "), g.ref.Fields[0], e.Name),
	}
	if self && c.satelliteTypesFit(g) {
		is.level = SeverityWarning
		is.measured += fmt.Sprintf(", and it refers to another %s, so the data belongs to "+
			"the pair", e.Canonical)
	}
	return is
}

// satelliteTypesFit reports whether every satellite's type fits its suffix:
// a date or time for when the relationship held, anything else for its role,
// terms, location or notes.
func (c *catalog) satelliteTypesFit(g *satelliteGroup) bool {
	return !slices.ContainsFunc(g.satellites, func(f *Field) bool {
		suffix := strings.TrimPrefix(snakeName(f.Name), g.stem+"_")
		return c.rx.whenSuffix[suffix] != isTemporal(f.Type)
	})
}

// check64B reports a junction that has grown attributes of its own and is
// used as an entity, yet is still keyed only by the references it links
// (rule_plan.md §6.4-B): another entity references it, or a query filters
// on one of its attributes.
func check64B(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		key := linkKey(e)
		if key == nil || outsideConventions(e) {
			continue
		}
		var attrs []string
		for i := range e.Fields {
			name := e.Fields[i].Name
			if !isKeyOrRef(e, name) && !c.rx.metadataName.MatchString(snakeName(name)) {
				attrs = append(attrs, name)
			}
		}
		if len(attrs) < junctionAttributes {
			continue
		}
		if use := c.entityUse(e, attrs); use != "" {
			out = append(out, issue{
				entity: e,
				measured: fmt.Sprintf("%s links by (%s) but has %d attributes of its own (%s), "+
					"and %s", e.Name, strings.Join(key, ", "), len(attrs),
					strings.Join(attrs, ", "), use),
				suggestion: fmt.Sprintf("Give it a surrogate key %s_id, keep (%s) UNIQUE, and "+
					"name it for the business relationship (enrollment, not student_course) "+
					"(§6.4).", snakeName(e.Canonical), strings.Join(key, ", ")),
			})
		}
	}
	return out
}

// linkKey returns the key of e made only of two or more reference columns:
// its primary key, or, with no primary key, its first such unique key.
func linkKey(e *Entity) []string {
	refs := refColumns(e)
	allRefs := func(k []string) bool {
		return len(k) >= 2 && !slices.ContainsFunc(k, func(col string) bool {
			return !containsFold(refs, col)
		})
	}
	if len(e.PrimaryKey) > 0 {
		if allRefs(e.PrimaryKey) {
			return e.PrimaryKey
		}
		return nil
	}
	for _, u := range e.Uniques {
		if allRefs(u) {
			return u
		}
	}
	return nil
}

// entityUse says how e is used as an entity, or "" when it is not: another
// entity references it, or a query filters on one of attrs.
func (c *catalog) entityUse(e *Entity, attrs []string) string {
	for _, o := range c.entities {
		if o != e && c.refersTo(o, e) {
			return o.Name + " references it"
		}
	}
	for i := range c.queries {
		q := &c.queries[i]
		s := c.scopeOf(q)
		for _, ref := range q.Filters {
			if t, f := s.resolve(ref); t == e && f != nil && containsFold(attrs, f.Name) {
				return fmt.Sprintf("a query at %s filters on %s", q.Pos, f.Name)
			}
		}
	}
	return ""
}

// check64C reports a change since mml.lock.json that widens an entity with
// data about one of its references (rule_plan.md §6.4-C): a new
// {stem}_since, _until, _role or _terms beside a reference it already had,
// or a reference split into {stem}_1_id and {stem}_2_id. It sees only what
// changed since the lock; 6.4-A and 6.4-B cover what was there before.
func check64C(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		old, ok := c.previousOf(e.ID)
		if !ok || outsideConventions(e) {
			continue
		}
		for i := range e.Fields {
			f := &e.Fields[i]
			if _, existed := old.Field(f.Name); existed {
				continue
			}
			if stem, ok := c.relationshipGrowth(old, f); ok {
				out = append(out, issue{
					entity: e, field: f.Name, pos: f.Pos,
					measured: fmt.Sprintf("this change adds %s to %s, data about its %s "+
						"relationship", f.Name, e.Name, stem),
					suggestion: fmt.Sprintf("Model the %s relationship as its own entity, "+
						"keyed by both sides, instead of widening %s (§6.4).", stem, e.Name),
				})
			}
		}
	}
	return out
}

// relationshipGrowth reports whether new field f, absent from old, adds
// data to a reference old already had, and which.
func (c *catalog) relationshipGrowth(old *Entity, f *Field) (string, bool) {
	name := snakeName(f.Name)
	for i := range old.Refs {
		stem := refStem(&old.Refs[i])
		if stem == "" {
			continue
		}
		suffix, ok := strings.CutPrefix(name, stem+"_")
		if ok && (c.rx.growth[suffix] || suffix == "1_id" || suffix == "2_id") {
			return stem, true
		}
	}
	return "", false
}
