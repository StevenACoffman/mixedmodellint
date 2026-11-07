package mixedmodellint

import "fmt"

// memberPair is two members of one logical entity from different sources,
// with the field of each that the same canonical name links.
type memberPair struct {
	a, b           *Entity
	aField, bField *Field
}

// logicalGroups returns each logical entity that more than one kind of
// source describes (a SQL table and its Go struct, say): its members, in
// catalog order, once per group. The cross-source designs (rule_plan.md
// §4.1-B, §5.3-C, §6.2-C, §9.1-C) compare members within a group.
//
// Ensures: every group has members from at least two source kinds, and no
// entity is in two groups.
func (c *catalog) logicalGroups() [][]*Entity {
	var out [][]*Entity
	seen := map[string]bool{}
	for _, e := range c.entities {
		if seen[e.Canonical] {
			continue
		}
		seen[e.Canonical] = true
		members := c.byCanonical[e.Canonical]
		kinds := map[SourceKind]struct{}{}
		for _, m := range members {
			kinds[m.Source] = struct{}{}
		}
		if len(kinds) >= 2 {
			out = append(out, members)
		}
	}
	return out
}

// linkedPairs returns, for every two members of group from different
// sources, each pair of their fields with the same canonical name, in
// member and field order. Each unordered pair of members appears once.
func linkedPairs(group []*Entity) []memberPair {
	var out []memberPair
	for i, a := range group {
		for _, b := range group[i+1:] {
			if a.Source == b.Source {
				continue
			}
			for j := range a.Fields {
				if bf := linkedField(b, &a.Fields[j]); bf != nil {
					out = append(out, memberPair{a: a, b: b, aField: &a.Fields[j], bField: bf})
				}
			}
		}
	}
	return out
}

// linkedField returns the field of e whose name folds to the same canonical
// name as f's (customer_id, CustomerID and customerId link), or nil.
func linkedField(e *Entity, f *Field) *Field {
	want := Canonical(f.Name)
	for i := range e.Fields {
		if Canonical(e.Fields[i].Name) == want {
			return &e.Fields[i]
		}
	}
	return nil
}

// memberName names a member in prose by its source and kind: "SQL table
// orders", "Go struct Order", "JSON Schema order".
func memberName(e *Entity) string {
	switch e.Source {
	case SourceGo:
		return "Go struct " + e.Name
	case SourceJSONSchema:
		return "JSON Schema " + e.Name
	default:
		return fmt.Sprintf("SQL %s %s", e.Kind, e.Name)
	}
}

// groupReference reports whether some member of group declares a reference
// covering a field linked to f: the field is a reference in the logical
// entity, which §6.2-C compares, so §5.3-C leaves it alone.
func groupReference(group []*Entity, f *Field) bool {
	want := Canonical(f.Name)
	for _, m := range group {
		for _, r := range m.Refs {
			for _, rf := range r.Fields {
				if !r.Inferred && Canonical(rf) == want {
					return true
				}
			}
		}
	}
	return false
}

// aType names a logical type with its article, for a message: "an int",
// "a string".
func aType(t LogicalType) string {
	switch name := t.String(); name[0] {
	case 'a', 'e', 'i', 'o', 'u':
		return "an " + name
	default:
		return "a " + name
	}
}
