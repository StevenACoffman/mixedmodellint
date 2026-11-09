package mixedmodellint

import (
	"fmt"
	"strings"
)

// minEmbedFields is the fewest concept fields an embedded shape and a stored
// record must each have before §6.5-A compares them: two or three generic
// names in common are a coincidence, not a copy.
const minEmbedFields = 3

// embedding is a by-value reference whose copy matches a record stored on
// its own: parent.field holds shape, which matches record with field-name
// overlap score (1 when shape is the record itself).
type embedding struct {
	parent *Entity
	field  string
	pos    Position
	shape  *Entity
	record *Entity
	score  float64
}

func section6EmbedDesigns() []design {
	return []design{{
		id: "6.5-A", title: "Shared-shape embedding detector", strength: Should,
		check: check65A,
	}}
}

// check65A reports a document that stores a copy of a shared record
// (rule_plan.md §6.5-A): a by-value reference whose target is a record kept
// on its own, or whose concept fields match one with Jaccard similarity of
// at least structureOverlap, the threshold §4.10 uses for the same
// question. The copy goes stale when the record changes. Value objects are
// exempt, and so is a record that references the parent, which the parent
// owns (an order's lines) rather than shares. An intentional snapshot is
// waived with reason "snapshot".
//
// Only shapes the loaders keep are compared: JSON Schema $ref targets and
// Go struct types. Anonymous inline objects are not loaded yet.
func check65A(c *catalog) []issue {
	var found []embedding
	for _, e := range c.entities {
		if outsideConventions(e) {
			continue
		}
		for i := range e.Refs {
			if em, ok := c.embeddedCopy(e, &e.Refs[i]); ok {
				found = append(found, em)
			}
		}
	}
	out := make([]issue, 0, len(found))
	for i := range found {
		out = append(out, embeddingIssue(&found[i], otherHolders(found, &found[i])))
	}
	return out
}

// embeddedCopy returns the stored record that e's by-value reference r
// copies, if any.
func (c *catalog) embeddedCopy(e *Entity, r *Ref) (embedding, bool) {
	shape, pos, ok := c.heldShape(e, r)
	if !ok {
		return embedding{}, false
	}
	em := embedding{parent: e, field: r.Fields[0], pos: pos, shape: shape, record: shape, score: 1}
	if !storedRecord(shape) {
		em.record, em.score = c.closestRecord(e, shape)
	}
	if em.record == nil || c.ownedBy(em.record, e) {
		return embedding{}, false
	}
	return em, true
}

// heldShape returns the entity whose copy e holds through r, and where e
// holds it; false when r is a reference by key or the copy is declared a
// value object, on its type or on the field.
func (c *catalog) heldShape(e *Entity, r *Ref) (*Entity, Position, bool) {
	if !r.ByValue || len(r.Fields) != 1 {
		return nil, Position{}, false
	}
	shape := c.lookup(r.Target, e.Source)
	if shape == nil || shape == e || shape.Annotations.Has("value-object") {
		return nil, Position{}, false
	}
	pos := r.Pos
	if f, ok := e.Field(r.Fields[0]); ok {
		if f.Annotations.Has("value-object") {
			return nil, Position{}, false
		}
		pos = f.Pos
	}
	return shape, pos, true
}

// closestRecord returns the stored record whose concept fields best match
// shape's, at structureOverlap or more, and the overlap; or nil.
func (c *catalog) closestRecord(parent, shape *Entity) (record *Entity, score float64) {
	fields := c.conceptFields(shape)
	if len(fields) < minEmbedFields {
		return nil, 0
	}
	for _, cand := range c.entities {
		if cand == shape || cand.Canonical == parent.Canonical || !storedRecord(cand) {
			continue
		}
		other := c.conceptFields(cand)
		if len(other) < minEmbedFields {
			continue
		}
		if j := jaccard(fields, other); j >= structureOverlap && j > score {
			record, score = cand, j
		}
	}
	return record, score
}

// storedRecord reports whether e is kept on its own, so a copy held
// elsewhere can go stale: a SQL table, a keyed Go struct (a BSON document
// type without a key is a nested shape), or the root of a JSON Schema
// document, whose ID ends at the fragment (see Entity). A $defs entry is a
// shape for reuse, not a record. A value object is never a record.
func storedRecord(e *Entity) bool {
	if e.Annotations.Has("value-object") {
		return false
	}
	switch e.Source {
	case SourceSQL:
		return e.Kind == KindTable
	case SourceGo:
		return len(e.PrimaryKey) > 0
	default:
		return strings.HasSuffix(e.ID, "#")
	}
}

// ownedBy reports whether record references parent's logical entity, as an
// order line references its order: the parent owns it, so embedding it is
// composition, not a copy of shared data.
func (c *catalog) ownedBy(record, parent *Entity) bool {
	for _, r := range record.Refs {
		if t := c.lookup(r.Target, record.Source); t != nil && t.Canonical == parent.Canonical {
			return true
		}
	}
	return false
}

// otherHolders names the other parents that embed a copy of em's record.
func otherHolders(found []embedding, em *embedding) []string {
	var out []string
	for i := range found {
		o := &found[i]
		if o.record == em.record && o.parent != em.parent && !containsFold(out, o.parent.Name) {
			out = append(out, o.parent.Name)
		}
	}
	return out
}

func embeddingIssue(em *embedding, others []string) issue {
	measured := fmt.Sprintf("%s.%s embeds a copy of %s", em.parent.Name, em.field,
		em.record.Name)
	if em.shape != em.record {
		measured += fmt.Sprintf(" (its shape %s matches at %.2f)", em.shape.Name, em.score)
	}
	if len(others) > 0 {
		measured += "; it is also embedded in " + andList(others)
	}
	key := singular(em.record.Canonical) + "_id"
	return issue{
		entity: em.parent, field: em.field, pos: em.pos,
		measured: measured + ", so each copy goes stale when " + em.record.Name + " changes",
		suggestion: fmt.Sprintf("Store %s and reference the shared record instead. If the "+
			"copy is a snapshot taken on purpose (§6.6), waive §6.5 with reason=\"snapshot\"; "+
			"if %s is a value with no identity, mark it %s (§6.5).", key, em.shape.Name,
			flagHint(em.shape, "value-object")),
	}
}
