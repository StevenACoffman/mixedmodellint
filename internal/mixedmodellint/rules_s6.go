package mixedmodellint

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

func section6Designs() []design {
	return []design{
		{id: "6.1-A", title: "Array-of-references detector", strength: Must, check: check61A},
		{
			id: "6.2-A", title: "Reference-shaped column without constraint", strength: Must,
			check: check62A,
		},
		{
			id: "6.2-B", title: "Mandatory-parent nullability contract", strength: Must,
			check: check62B,
		},
	}
}

// foreignSchema reports whether e's structure belongs to someone else, so
// relationship rules it cannot follow go to §3.5 instead.
func foreignSchema(e *Entity) bool {
	src, _ := e.Annotations.Get("source")
	return src == "legacy" || src == "third_party"
}

func check61A(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		if foreignSchema(e) || ownerOnlyDocument(e) {
			continue
		}
		for i := range e.Fields {
			f := &e.Fields[i]
			if target, ok := c.referenceList(e, f); ok {
				out = append(out, issue{
					entity: e, field: f.Name, pos: f.Pos,
					measured: fmt.Sprintf("%s.%s stores a list of %s references, so the pairs "+
						"cannot be constrained, indexed or joined", e.Name, f.Name, target),
					suggestion: fmt.Sprintf("Replace it with a junction table %s_%s(%s_id "+
						"REFERENCES %s, %s_id REFERENCES %s, PRIMARY KEY (%s_id, %s_id)), one row "+
						"per pair (§6.1).", e.Canonical, target, e.Canonical, e.Name, target,
						target, e.Canonical, target),
				})
			}
		}
	}
	return out
}

// ownerOnlyDocument reports a document read only by the entity's owner,
// which §6.5 governs instead: one declared consumer and a private layer.
func ownerOnlyDocument(e *Entity) bool {
	consumers, ok := e.Annotations.Get("consumers")
	if !ok || len(splitList(consumers)) != 1 {
		return false
	}
	layer, _ := e.Annotations.Get("layer")
	switch layer {
	case "core", "analytical", "obt", "feature", "reference":
		return false
	default:
		return true
	}
}

// referenceList reports whether f is an array holding references: named
// like a list of IDs, or an array of another entity that does not reference
// e back (a has-many through a back reference is not a list of references).
// It returns what the references point at.
func (c *catalog) referenceList(e *Entity, f *Field) (string, bool) {
	if f.Type != TypeArray {
		return "", false
	}
	for _, r := range e.Refs {
		if !containsFold(r.Fields, f.Name) {
			continue
		}
		t := c.lookup(r.Target, e.Source)
		if t == nil || slices.Contains(c.parents(t), e) {
			return "", false
		}
		return t.Canonical, true
	}
	ws := splitWords(f.Name)
	if len(ws) > 1 && (ws[len(ws)-1] == "ids" || ws[len(ws)-1] == "id") {
		return strings.Join(ws[:len(ws)-1], "_"), true
	}
	return "", false
}

func check62A(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		if foreignSchema(e) {
			continue
		}
		for i := range e.Fields {
			f := &e.Fields[i]
			if target := c.undeclaredReference(e, f); target != nil {
				out = append(out, issue{
					entity: e, field: f.Name, pos: f.Pos,
					measured: fmt.Sprintf("%s.%s refers to %s but has no foreign key, so it "+
						"can hold IDs of rows that do not exist", e.Name, f.Name, target.Name),
					suggestion: fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT fk_%s_%s FOREIGN KEY "+
						"(%s) REFERENCES %s(%s); (Go: a gorm foreignKey tag or mml:\"ref=%s\"; "+
						"JSON: x-mml-ref) (§6.2).", e.Name, e.Name, f.Name, f.Name, target.Name,
						strings.Join(target.PrimaryKey, ", "), target.Name),
				})
			}
		}
	}
	return out
}

// undeclaredReference returns the entity f is named after (customer_id
// refers to customer; parent_id to e itself) when no declared reference
// covers f, here or on an entity linked to e. A polymorphic subject_id with a
// subject_type sibling, an ID that names nothing in the catalog, and e's own
// key return nil.
func (c *catalog) undeclaredReference(e *Entity, f *Field) *Entity {
	stem, ok := strings.CutSuffix(Canonical(f.Name), "_id")
	if !ok || stem == "" {
		return nil
	}
	if _, poly := e.Field(stem + "_type"); poly {
		return nil
	}
	target := c.lookup(stem, e.Source)
	if stem == "parent" {
		target = e
	}
	if target == nil || (target == e && stem != "parent") || c.declaresReference(e, f.Name) {
		return nil
	}
	return target
}

// declaresReference reports whether e, or an entity of another source linked
// to it, declares a reference covering column. An inferred reference (a Go
// XxxID next to a struct Xxx) is a guess, not a declaration.
func (c *catalog) declaresReference(e *Entity, column string) bool {
	for _, linked := range c.byCanonical[e.Canonical] {
		for _, r := range linked.Refs {
			if !r.Inferred && containsFold(r.Fields, column) {
				return true
			}
		}
	}
	return false
}

func check62B(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		for i := range e.Refs {
			r := &e.Refs[i]
			if r.Inferred {
				continue
			}
			if is, ok := optionalParent(e, r); ok {
				out = append(out, is)
			}
		}
	}
	return out
}

// optionalParent reports a nullable reference column whose cardinality is
// mandatory (card=1..N) or not declared at all: optionality has to be a
// recorded decision (rule_plan.md §6.2-B).
func optionalParent(e *Entity, r *Ref) (issue, bool) {
	var nullable *Field
	for _, col := range r.Fields {
		if f, ok := e.Field(col); ok && f.Nullable {
			nullable = f
			break
		}
	}
	if nullable == nil {
		return issue{}, false
	}
	card, declared := nullable.Annotations.Get("card")
	if declared && cardinalityLow(card) == 0 {
		return issue{}, false
	}
	kind := "an undeclared-cardinality"
	if declared {
		kind = "a mandatory"
	}
	return issue{
		entity: e, field: nullable.Name, pos: nullable.Pos,
		measured: fmt.Sprintf("%s.%s is %s reference to %s but allows NULL, so orphans are "+
			"possible", e.Name, nullable.Name, kind, r.Target),
		suggestion: fmt.Sprintf("Add NOT NULL (Go: a value instead of a pointer; JSON: list "+
			"%q in required), or declare card=0..N with null=not_applicable if the parent is "+
			"truly optional (§6.2).", nullable.Name),
	}, true
}

// cardinalityLow returns the lower bound of a cardinality such as "1..N",
// "0..1" or "1"; an unreadable one counts as mandatory (1), so a typo cannot
// make a reference optional.
func cardinalityLow(card string) int {
	low, _, _ := strings.Cut(strings.TrimSpace(card), "..")
	n, err := strconv.Atoi(strings.TrimSpace(low))
	if err != nil {
		return 1
	}
	return n
}
