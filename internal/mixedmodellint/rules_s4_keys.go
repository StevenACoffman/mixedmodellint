package mixedmodellint

import (
	"fmt"
	"strings"
)

// The parts a primary-key column plays, for §4.4.
const (
	naturalPart keyPart = iota
	surrogatePart
	foreignPart
	ordinalPart
	periodPart
)

// keyPart is what a primary-key column holds: a value the system generated,
// a reference, a position within a parent, a calendar period, or a value
// from the world.
type keyPart int

func section4KeyDesigns() []design {
	return []design{
		{
			id: "4.4-A", title: "Primary-key composition classifier", strength: Should,
			check: check44A,
		},
		{
			id: "4.4-B", title: "Natural/legacy IDs must be indexed attributes", strength: Should,
			check: check44B,
		},
		{
			id: "4.5-A", title: "Reference-by-display-attribute detector", strength: Should,
			check: check45A,
		},
		{id: "4.7-A", title: "Content-derived key detector", strength: Should, check: check47A},
	}
}

// keyPartOf classifies column col of e's primary key. A column the database
// generates, or an integer or UUID named as an ID, is surrogate: Go and JSON
// cannot show a generated default, so the name stands in for it there. A
// line ordinal, or a validity start or version beside a reference, numbers
// rows within a parent rather than naming something in the world. A date is
// the calendar bucket a roll-up or snapshot is grouped by (§8.5-B asks for
// exactly that key), not an identifier.
func (c *catalog) keyPartOf(e *Entity, col string) keyPart {
	f, ok := e.Field(col)
	switch {
	case containsFold(refColumns(e), col):
		return foreignPart
	case c.rx.lineOrdinal.MatchString(col):
		return ordinalPart
	case !ok:
		return naturalPart
	case f.Type == TypeDate:
		return periodPart
	case c.generatedKey(f):
		return surrogatePart
	default:
		return naturalPart
	}
}

// generatedKey reports whether the system generates f's values: by a
// default or serial type, or, where a default cannot be seen, an integer or
// UUID named as an ID.
func (c *catalog) generatedKey(f *Field) bool {
	if c.rx.surrogate.MatchString(f.Default) ||
		strings.Contains(strings.ToLower(f.RawType), "serial") {
		return true
	}
	ws := splitWords(f.Name)
	return (f.Type == TypeInt || f.Type == TypeUUID) && len(ws) > 0 &&
		(ws[len(ws)-1] == "id" || ws[len(ws)-1] == "uuid")
}

// refColumns returns every column of e's references.
func refColumns(e *Entity) []string {
	var cols []string
	for _, r := range e.Refs {
		cols = append(cols, r.Fields...)
	}
	return cols
}

// check44A reports a primary key made of natural values (rule_plan.md
// §4.4-A): a composite key with a natural column, or a single natural column
// on an entity that merges records from several systems. A key made only of
// references is a junction's and passes. Stored aggregates are keyed by
// their dimensions on purpose (§8.5), so they and views are not checked. The
// finding counts the references that repeat a composite key (Design C).
func check44A(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		if e.Kind == KindView || foreignSchema(e) || c.storedAggregate(e) {
			continue
		}
		if is, ok := c.naturalPrimaryKey(e); ok {
			out = append(out, is)
		}
	}
	return out
}

// storedAggregate reports whether e holds rows rolled up to a coarser grain,
// whose key is the dimensions it is grouped by: by its layer, its declared
// dimensions, or a grain per calendar period.
func (c *catalog) storedAggregate(e *Entity) bool {
	layer, _ := e.Annotations.Get("layer")
	grain, _ := e.Annotations.Get("grain")
	return layer == "analytical" || layer == "obt" || layer == "feature" ||
		e.Annotations.Has("dimensions") || c.rx.periodGrain.MatchString(grain)
}

// naturalPrimaryKey applies §4.4-A to e.
func (c *catalog) naturalPrimaryKey(e *Entity) (issue, bool) {
	var natural []string
	for _, k := range e.PrimaryKey {
		if c.keyPartOf(e, k) == naturalPart {
			natural = append(natural, k)
		}
	}
	if len(natural) == 0 {
		return issue{}, false
	}
	cols := strings.Join(e.PrimaryKey, ", ")
	is := issue{
		entity: e,
		suggestion: fmt.Sprintf("Add %s_id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY "+
			"and keep (%s) as a unique index for lookups (§4.4).", snakeName(e.Canonical), cols),
	}
	if len(e.PrimaryKey) == 1 {
		systems, multi := c.sourceSystems(e)
		if !multi {
			return issue{}, false
		}
		is.measured = fmt.Sprintf("%s is keyed by the natural value %s, but it merges records "+
			"from several systems (%s), which can disagree about it", e.Name, cols,
			systemList(systems))
		return is, true
	}
	is.measured = fmt.Sprintf("%s's primary key (%s) is composite and includes natural "+
		"values (%s)", e.Name, cols, strings.Join(natural, ", "))
	if carriers := c.compositeCarriers(e); len(carriers) > 0 {
		is.measured += fmt.Sprintf("; references from %s repeat all %d key columns, where a "+
			"surrogate key would need one", strings.Join(carriers, ", "), len(e.PrimaryKey))
	}
	return is, true
}

// compositeCarriers names the entities whose references to e repeat every
// column of its composite primary key.
func (c *catalog) compositeCarriers(e *Entity) []string {
	var out []string
	for _, o := range c.entities {
		for _, r := range o.Refs {
			if len(r.Fields) == len(e.PrimaryKey) && c.lookup(r.Target, o.Source) == e {
				out = append(out, o.Name)
				break
			}
		}
	}
	return out
}

// check44B reports natural and legacy identifiers (an ERP ID, an email, a
// SKU) on an entity with a surrogate key that no index or unique key leads,
// so a lookup by them scans the table (rule_plan.md §4.4-B). Only SQL tables
// are checked: Go and JSON Schema have no index to look for.
func check44B(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		if e.Source != SourceSQL || foreignSchema(e) || len(e.PrimaryKey) != 1 ||
			c.keyPartOf(e, e.PrimaryKey[0]) != surrogatePart {
			continue
		}
		for i := range e.Fields {
			f := &e.Fields[i]
			if !c.rx.naturalID.MatchString(f.Name) && !f.Annotations.Has("natural_key") ||
				leadsIndex(e, f.Name) {
				continue
			}
			out = append(out, issue{
				entity: e, field: f.Name, pos: f.Pos,
				measured: fmt.Sprintf("%s.%s looks like a natural or legacy identifier, but no "+
					"index or unique key starts with it", e.Name, f.Name),
				suggestion: fmt.Sprintf("Add CREATE UNIQUE INDEX ON %s (%s), or a non-unique "+
					"index if duplicates are expected (§4.4).", e.Name, f.Name),
			})
		}
	}
	return out
}

// leadsIndex reports whether col is the first column of one of e's unique
// keys or indexes.
func leadsIndex(e *Entity, col string) bool {
	for _, cols := range append(append([][]string(nil), e.Uniques...), e.Indexes...) {
		if len(cols) > 0 && strings.EqualFold(cols[0], col) {
			return true
		}
	}
	return false
}

// check45A reports a field that identifies another entity by a display value
// that can change, such as customer_name or author_email, beside no ID of it
// (rule_plan.md §4.5-A). A role (reviewer_name) names no entity, so it needs
// no matching entity to be reported; refers=<entity> says which it is.
func check45A(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		if foreignSchema(e) {
			continue
		}
		for i := range e.Fields {
			if is, ok := c.displayReference(e, &e.Fields[i]); ok {
				out = append(out, is)
			}
		}
	}
	return out
}

// displayReference applies §4.5-A to one field.
func (c *catalog) displayReference(e *Entity, f *Field) (issue, bool) {
	stem, target := c.displayTarget(e, f)
	if stem == "" || hasIDOf(e, stem) || target != nil && c.refersTo(e, target) {
		return issue{}, false
	}
	other, key := stem, stem+"_id"
	if target != nil {
		other = target.Name
		if len(target.PrimaryKey) == 1 {
			key = target.PrimaryKey[0]
		}
	}
	return issue{
		entity: e, field: f.Name, pos: f.Pos,
		measured: fmt.Sprintf("%s.%s identifies a %s by a display value that can change, and "+
			"%s has no %s_id", e.Name, f.Name, stem, e.Name, stem),
		suggestion: fmt.Sprintf("Add %s_id referencing %s.%s and keep %s only as a "+
			"denormalized label (§4.5).", stem, other, key, f.Name),
	}, true
}

// displayTarget returns the stem of f's name when f is a display value of
// another entity or of a role, and the entity it identifies, if known. stem
// is "" when f is no such value, including an entity's own name.
func (c *catalog) displayTarget(e *Entity, f *Field) (stem string, target *Entity) {
	name := snakeName(f.Name)
	target = c.declaredTarget(e, f)
	if m := c.rx.roleDisplay.FindStringSubmatch(name); m != nil {
		return m[1], target
	}
	m := c.rx.displayRef.FindStringSubmatch(name)
	if m == nil {
		return "", nil
	}
	if target == nil {
		target = c.lookup(m[1], e.Source)
	}
	if target == nil || target == e {
		return "", nil
	}
	return m[1], target
}

// declaredTarget returns the entity f's refers annotation names, or nil.
func (c *catalog) declaredTarget(e *Entity, f *Field) *Entity {
	if name, ok := f.Annotations.Get("refers"); ok {
		return c.lookup(name, e.Source)
	}
	return nil
}

// hasIDOf reports whether e has a {stem}_id field in any case style.
func hasIDOf(e *Entity, stem string) bool {
	for i := range e.Fields {
		if strings.EqualFold(snakeName(e.Fields[i].Name), stem+"_id") {
			return true
		}
	}
	return false
}

// refersTo reports whether one of e's references targets t.
func (c *catalog) refersTo(e, t *Entity) bool {
	for _, r := range e.Refs {
		if c.lookup(r.Target, e.Source) == t {
			return true
		}
	}
	return false
}

// check47A reports keys derived from content (rule_plan.md §4.7-A): a
// primary-key column named as a hash on any entity, and on a file entity a
// key that is a file name or path or is not a UUID. Editing a file changes
// its hash and renaming it changes its path, so either breaks every
// reference. A content-addressed store, role=blob_store, is keyed by hash on
// purpose.
func check47A(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		role, _ := e.Annotations.Get("role")
		if foreignSchema(e) || role == "blob_store" {
			continue
		}
		if col, reason := c.contentKey(e); reason != "" {
			f, _ := e.Field(col)
			is := issue{
				entity: e, field: col,
				measured: fmt.Sprintf("%s is keyed by %s, %s", e.Name, col, reason),
				suggestion: fmt.Sprintf("Use %s_id UUID PRIMARY KEY DEFAULT gen_random_uuid() "+
					"and keep %s as a non-key attribute (for a hash, content_sha256 CHAR(64)), "+
					"or key a separate %s_versions table by it (§4.7).", snakeName(e.Canonical),
					col, snakeName(e.Canonical)),
			}
			if f != nil {
				is.pos = f.Pos
			}
			out = append(out, is)
		}
	}
	return out
}

// contentKey returns the primary-key column of e that is derived from
// content, and why that breaks references, or "" for the reason when there
// is none.
func (c *catalog) contentKey(e *Entity) (col, reason string) {
	for _, k := range e.PrimaryKey {
		if c.rx.hashName.MatchString(snakeName(k)) {
			return k, "a hash of its content, which changes whenever the content is edited " +
				"and orphans every reference"
		}
	}
	if len(e.PrimaryKey) != 1 || !fileEntity(c, e) {
		return "", ""
	}
	k := e.PrimaryKey[0]
	switch f, ok := e.Field(k); {
	case c.rx.fileName.MatchString(k) || c.rx.fileLocation.MatchString(k):
		return k, "a file name or path, which changes when the file is renamed or moved " +
			"and orphans every reference"
	case ok && f.Type != TypeUUID:
		return k, "which is not a UUID; a file's identity should be a UUID that survives " +
			"renames, edits and copies between stores"
	default:
		return "", ""
	}
}
