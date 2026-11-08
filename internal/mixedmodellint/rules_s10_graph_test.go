package mixedmodellint_test

import (
	"testing"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// fk is a declared, enforced reference from field to target's key.
func fk(field, target, onDelete string) m.Ref {
	return m.Ref{Fields: []string{field}, Target: target, OnDelete: onDelete}
}

func TestEdgeEndpoints(t *testing.T) {
	t.Parallel()
	nodes := table("nodes", []string{"node_id"}, col("node_id", m.TypeInt))
	types := table("relationship_types", []string{"relationship_type_id"},
		col("relationship_type_id", m.TypeInt))
	edges := func(fields []m.Field, refs ...m.Ref) m.Entity {
		e := table("edges", []string{"edge_id"},
			append([]m.Field{col("edge_id", m.TypeInt)}, fields...)...)
		e.Refs = refs
		return e
	}
	sound := []m.Field{
		col("from_node_id", m.TypeInt), col("to_node_id", m.TypeInt),
		col("relationship_type_id", m.TypeInt),
	}
	typeRef := fk("relationship_type_id", "relationship_types", "restrict")
	cases := map[string]struct {
		entities []m.Entity
		want     []string
	}{
		"enforced, required, cascading endpoints": {
			entities: []m.Entity{
				nodes,
				types,
				edges(
					sound,
					fk(
						"from_node_id",
						"nodes",
						"cascade",
					),
					fk("to_node_id", "nodes", "cascade"),
					typeRef,
				),
			},
		},
		"an endpoint with no foreign key": {
			entities: []m.Entity{nodes, types, edges(sound,
				fk("from_node_id", "nodes", "cascade"), typeRef)},
			want: []string{"10.10-A edges to_node_id"},
		},
		"a nullable endpoint": {
			entities: []m.Entity{
				nodes,
				types,
				edges(
					[]m.Field{nullable(&sound[0]), sound[1], sound[2]},
					fk(
						"from_node_id",
						"nodes",
						"cascade",
					),
					fk("to_node_id", "nodes", "cascade"),
					typeRef,
				),
			},
			want: []string{"10.10-A edges from_node_id"},
		},
		"ON DELETE SET NULL": {
			entities: []m.Entity{
				nodes,
				types,
				edges(
					sound,
					fk(
						"from_node_id",
						"nodes",
						"set null",
					),
					fk("to_node_id", "nodes", "cascade"),
					typeRef,
				),
			},
			want: []string{"10.10-A edges from_node_id"},
		},
		"a polymorphic endpoint": {
			entities: []m.Entity{nodes, edges([]m.Field{
				col("source_id", m.TypeInt), col("target_id", m.TypeInt),
				col("target_type", m.TypeString), col("rel_type", m.TypeString),
			}, fk("source_id", "nodes", "cascade"))},
			want: []string{"10.10-A edges target_id"},
		},
		"a junction table is no edge table": {
			entities: []m.Entity{
				table("orders", []string{"order_id"}, col("order_id", m.TypeInt)),
				table("products", []string{"product_id"}, col("product_id", m.TypeInt)),
				table("order_lines", nil, col("order_id", m.TypeInt), col("product_id", m.TypeInt)),
			},
		},
		"declared an edge": {
			entities: []m.Entity{func() m.Entity {
				e := entityOf("follows", []string{"graph", "edge"},
					col("follower_id", m.TypeInt), col("followee_id", m.TypeInt))
				return e
			}()},
			want: []string{"10.10-A follows followee_id", "10.10-A follows follower_id"},
		},
		"a JSON Schema edge with $ref endpoints": {
			entities: []m.Entity{jsonEdge(true)},
		},
		"a JSON Schema edge with bare string IDs": {
			entities: []m.Entity{jsonEdge(false)},
			want:     []string{"10.10-A Edge from_id", "10.10-A Edge to_id"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.want, findingsOf(src(tc.entities...), "10.10-A"))
		})
	}
}

// jsonEdge is a JSON Schema edge whose endpoints are $refs when refs is
// true, and bare string IDs otherwise.
func jsonEdge(refs bool) m.Entity {
	e := m.Entity{
		ID: "json:schemas/edge.json#", Kind: m.KindSchema, Source: m.SourceJSONSchema,
		Name: "Edge", Annotations: ann("graph", "edge"),
		Fields: []m.Field{
			col("from_id", m.TypeString), col("to_id", m.TypeString),
			col("predicate", m.TypeString),
		},
	}
	if refs {
		e.Refs = []m.Ref{
			{Fields: []string{"from_id"}, Target: "Node"},
			{Fields: []string{"to_id"}, Target: "Node"},
		}
	}
	return e
}

func TestOntologyExtensibility(t *testing.T) {
	t.Parallel()
	people := entityOf("people", []string{"graph", "node"}, col("person_id", m.TypeInt))
	people.PrimaryKey = []string{"person_id"}
	companies := entityOf("companies", []string{"graph", "node"}, col("company_id", m.TypeInt))
	companies.PrimaryKey = []string{"company_id"}
	link := func(name string) m.Entity {
		e := table(name, []string{"person_id", "company_id"},
			col("person_id", m.TypeInt), col("company_id", m.TypeInt))
		e.Refs = []m.Ref{
			fk("person_id", "people", "cascade"),
			fk("company_id", "companies", "cascade"),
		}
		return e
	}
	edges := func(rel m.Field, refs ...m.Ref) m.Entity {
		e := table("edges", []string{"edge_id"}, col("edge_id", m.TypeInt),
			col("from_node_id", m.TypeInt), col("to_node_id", m.TypeInt), rel)
		e.Refs = append([]m.Ref{
			fk("from_node_id", "people", "cascade"), fk("to_node_id", "people", "cascade"),
		}, refs...)
		return e
	}
	cases := map[string]struct {
		entities []m.Entity
		want     []string
		severity m.Severity
	}{
		"a table per relationship between graph nodes": {
			entities: []m.Entity{people, companies, link("employments"), link("board_seats")},
			want:     []string{"10.10-B board_seats"},
			severity: m.SeverityWarning,
		},
		"one table per pair is fine": {
			entities: []m.Entity{people, companies, link("employments")},
		},
		"junction tables outside a graph": {
			entities: []m.Entity{
				table("people", []string{"person_id"}, col("person_id", m.TypeInt)),
				table("companies", []string{"company_id"}, col("company_id", m.TypeInt)),
				link("employments"), link("board_seats"),
			},
		},
		"relationship types as a closed list": {
			entities: []m.Entity{
				people,
				edges(enumField("rel_type", []string{"WORKS_AT", "KNOWS"})),
			},
			want:     []string{"10.10-B edges rel_type"},
			severity: m.SeverityNote,
		},
		"relationship types as data": {
			entities: []m.Entity{
				people,
				table("relationship_types", []string{"relationship_type_id"},
					col("relationship_type_id", m.TypeInt)),
				edges(col("relationship_type_id", m.TypeInt),
					fk("relationship_type_id", "relationship_types", "restrict")),
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := src(tc.entities...)
			equals(t, tc.want, findingsOf(in, "10.10-B"))
			for _, f := range m.Analyze(in).Findings {
				if f.Design == "10.10-B" {
					equals(t, tc.severity, f.Severity)
				}
			}
		})
	}
}
