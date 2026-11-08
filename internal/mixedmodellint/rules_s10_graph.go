package mixedmodellint

import (
	"fmt"
	"slices"
	"strings"
)

func section10GraphDesigns() []design {
	return []design{
		{id: "10.10-A", title: "Edge endpoint integrity", strength: Should, check: check1010A},
		{id: "10.10-B", title: "Ontology extensibility check", strength: Should, check: check1010B},
	}
}

// check1010A reports edge endpoints that can point at a node that no longer
// exists: no enforced foreign key (a polymorphic endpoint can have none), a
// nullable endpoint, or ON DELETE SET NULL or SET DEFAULT, each of which
// lets deleting a node leave a dangling or half-attached edge (§10.10). A Go
// or JSON Schema edge with a SQL form is judged by that form, which is where
// integrity is enforced.
func check1010A(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		if !c.edgeTable(e) || e.Source != SourceSQL && c.hasSQLForm(e) {
			continue
		}
		for _, f := range c.endpoints(e) {
			if problems := c.endpointProblems(e, f); len(problems) > 0 {
				out = append(out, issue{
					entity: e, field: f.Name, pos: f.Pos,
					measured: fmt.Sprintf("%s.%s can point at a node that no longer exists: "+
						"it %s", e.Name, f.Name, strings.Join(problems, ", and it ")),
					suggestion: c.endpointFix(e, f),
				})
			}
		}
	}
	return out
}

// edgeTable reports whether e is a graph's edge: declared graph=edge, or
// with two or more endpoints and a relationship-type field or an edge-like
// name.
func (c *catalog) edgeTable(e *Entity) bool {
	if g, _ := e.Annotations.Get("graph"); g == "edge" {
		return true
	}
	if len(c.endpoints(e)) < 2 {
		return false
	}
	return c.relTypeField(e) != nil || c.rx.edgeName.MatchString(snakeName(e.Name))
}

// endpoints returns e's fields that identify a node: referencing fields and
// *_id fields, except e's own single-column key and the relationship type.
func (c *catalog) endpoints(e *Entity) []*Field {
	var out []*Field
	for i := range e.Fields {
		f := &e.Fields[i]
		name := snakeName(f.Name)
		switch {
		case c.rx.relTypeField.MatchString(name):
		case len(e.PrimaryKey) == 1 && strings.EqualFold(e.PrimaryKey[0], f.Name):
		case c.declaredRef(e, f) != nil || strings.HasSuffix(name, "_id"):
			out = append(out, f)
		}
	}
	return out
}

// relTypeField returns the field naming e's relationship type, or nil.
func (c *catalog) relTypeField(e *Entity) *Field {
	for i := range e.Fields {
		if c.rx.relTypeField.MatchString(snakeName(e.Fields[i].Name)) {
			return &e.Fields[i]
		}
	}
	return nil
}

// declaredRef returns the declared (not inferred) reference covering f, or
// nil.
func (c *catalog) declaredRef(e *Entity, f *Field) *Ref {
	for i := range e.Refs {
		if !e.Refs[i].Inferred && containsFold(e.Refs[i].Fields, f.Name) {
			return &e.Refs[i]
		}
	}
	return nil
}

// hasSQLForm reports whether a SQL table is a form of the same logical
// entity as e.
func (c *catalog) hasSQLForm(e *Entity) bool {
	return slices.ContainsFunc(c.byCanonical[e.Canonical], func(m *Entity) bool {
		return m.Source == SourceSQL
	})
}

// endpointProblems lists what lets endpoint f of edge e dangle, each as a
// verb phrase.
func (c *catalog) endpointProblems(e *Entity, f *Field) []string {
	r := c.declaredRef(e, f)
	if e.Source != SourceSQL {
		if r == nil && !f.Annotations.Has("ref") {
			return []string{"is a bare ID with no $ref or x-mml-ref to a node"}
		}
		return nil
	}
	var problems []string
	switch {
	case r == nil && polymorphicEndpoint(e, f):
		problems = append(problems, "is polymorphic, so no foreign key can enforce it")
	case r == nil:
		problems = append(problems, "has no foreign key")
	case r.OnDelete == "set null" || r.OnDelete == "set default":
		problems = append(problems, "is "+strings.ToUpper(r.OnDelete)+" when its node is deleted")
	}
	if f.Nullable {
		problems = append(problems, "may be NULL")
	}
	return problems
}

// polymorphicEndpoint reports whether endpoint f can point at several kinds
// of node: target_id beside a target_type.
func polymorphicEndpoint(e *Entity, f *Field) bool {
	stem, _ := strings.CutSuffix(snakeName(f.Name), "_id")
	_, ok := e.Field(stem + "_type")
	return ok
}

// endpointFix suggests how to make endpoint f of edge e unable to dangle.
func (c *catalog) endpointFix(e *Entity, f *Field) string {
	if e.Source != SourceSQL {
		return fmt.Sprintf("Make %s a $ref to the node definition, or declare x-mml-ref "+
			"(§10.10).", f.Name)
	}
	if polymorphicEndpoint(e, f) {
		return fmt.Sprintf("Reference one shared nodes(node_id) table from %s and keep the "+
			"kind as a node attribute (§10.10).", f.Name)
	}
	target := "<node>(<key>)"
	if r := c.declaredRef(e, f); r != nil {
		if t := c.lookup(r.Target, e.Source); t != nil && len(t.PrimaryKey) > 0 {
			target = t.Name + "(" + strings.Join(t.PrimaryKey, ", ") + ")"
		}
	}
	return fmt.Sprintf("Declare %s NOT NULL REFERENCES %s ON DELETE CASCADE (or RESTRICT, "+
		"if deletes must re-point edges first) (§10.10).", f.Name, target)
}

// check1010B reports where a graph's relationship types are fixed in its
// structure (§10.10): two or more tables connecting the same graph nodes,
// one per relationship, so a new relationship needs a new table; or, as a
// note, relationship types kept as a closed enum or CHECK list, which still
// needs a migration. A type that references a table of types passes.
func check1010B(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		if !c.edgeTable(e) {
			continue
		}
		if rf := c.relTypeField(e); rf != nil && len(enumValues(e, rf)) > 0 {
			out = append(out, issue{
				entity: e, field: rf.Name, pos: rf.Pos, level: SeverityNote,
				measured: fmt.Sprintf("%s.%s fixes the relationship types to a closed list, so "+
					"adding one takes a migration", e.Name, rf.Name),
				suggestion: "Keep relationship types as rows of relationship_types(" +
					"relationship_type_id, name) and reference it, so a new type is a new row " +
					"(§10.10).",
			})
		}
	}
	return append(out, c.typedEdgeTables()...)
}

// typedEdgeTables reports every table after the first that connects the
// same set of graph nodes as an earlier one.
func (c *catalog) typedEdgeTables() []issue {
	nodes := c.graphNodes()
	var out []issue
	firstFor := map[string]*Entity{}
	for _, e := range c.entities {
		targets := c.nodeTargets(e, nodes)
		if len(targets) < 2 || c.relTypeField(e) != nil {
			continue
		}
		key := strings.Join(targets, ", ")
		first, seen := firstFor[key]
		if !seen {
			firstFor[key] = e
			continue
		}
		out = append(out, issue{
			entity: e,
			measured: fmt.Sprintf("%s and %s both connect %s, one table per relationship, so a "+
				"new relationship type needs a new table", first.Name, e.Name, key),
			suggestion: "Use one edge table edges(edge_id, from_node_id, to_node_id, " +
				"relationship_type_id REFERENCES relationship_types) and add relationship types " +
				"as rows (§10.10).",
		})
	}
	return out
}

// graphNodes returns the entities that are a graph's nodes: declared
// graph=node, or referenced by an edge table's endpoints.
func (c *catalog) graphNodes() map[*Entity]bool {
	out := map[*Entity]bool{}
	for _, e := range c.entities {
		if g, _ := e.Annotations.Get("graph"); g == "node" {
			out[e] = true
		}
		if !c.edgeTable(e) {
			continue
		}
		for _, f := range c.endpoints(e) {
			if r := c.declaredRef(e, f); r != nil {
				if t := c.lookup(r.Target, e.Source); t != nil {
					out[t] = true
				}
			}
		}
	}
	return out
}

// nodeTargets returns the sorted names of the graph nodes e's declared
// references reach, when all of them are graph nodes, or nil.
func (c *catalog) nodeTargets(e *Entity, nodes map[*Entity]bool) []string {
	var names []string
	for _, r := range e.Refs {
		t := c.lookup(r.Target, e.Source)
		if r.Inferred || t == nil || !nodes[t] {
			return nil
		}
		names = append(names, t.Name)
	}
	slices.Sort(names)
	return names
}
