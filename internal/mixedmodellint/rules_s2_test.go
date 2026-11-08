package mixedmodellint_test

import (
	"slices"
	"testing"

	"pgregory.net/rapid"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// inSchema builds a SQL table in schema, so its ID carries the schema as
// the postgres adapter writes it.
func inSchema(schema, name string, pk []string, fields ...m.Field) m.Entity {
	e := table(name, pk, fields...)
	e.ID = "sql:" + schema + "." + name
	return e
}

// referencing returns a copy of e with a declared reference from field to
// target.
func referencing(e *m.Entity, field, target string) m.Entity {
	out := *e
	out.Refs = append(slices.Clone(e.Refs), m.Ref{Fields: []string{field}, Target: target})
	return out
}

func TestBoundaryTranslation(t *testing.T) {
	t.Parallel()
	customers := inSchema("crm", "customers", []string{"customer_id"},
		col("customer_id", m.TypeInt))
	tickets := func(schema string, customer m.Field) m.Entity {
		return referencing(
			new(inSchema(
				schema,
				"tickets",
				[]string{"ticket_id"},
				col("ticket_id", m.TypeInt),
				customer,
			)),
			"customer_id",
			"crm.customers",
		)
	}
	regions := inSchema("geo", "regions", []string{"region_id"},
		col("region_id", m.TypeInt), col("parent_id", m.TypeInt))
	regions = referencing(&regions, "parent_id", "geo.regions")
	regions.Annotations = ann("layer", "reference")
	goStruct := func(pkg, name string, fields ...m.Field) m.Entity {
		return m.Entity{
			ID: "go:example.com/" + pkg + "." + name, Kind: m.KindStruct, Source: m.SourceGo,
			Name: name, Fields: fields, Annotations: m.Annotations{},
		}
	}
	cases := map[string]struct {
		entities []m.Entity
		want     []string
	}{
		"a reference into another schema": {
			entities: []m.Entity{customers, tickets("support", col("customer_id", m.TypeInt))},
			want:     []string{"2.2-A tickets customer_id"},
		},
		"a reference within one schema": {
			entities: []m.Entity{customers, tickets("crm", col("customer_id", m.TypeInt))},
		},
		"unqualified tables have no domain": {
			entities: []m.Entity{
				table("customers", []string{"customer_id"}, col("customer_id", m.TypeInt)),
				referencing(new(table("tickets", nil, col("customer_id", m.TypeInt))),
					"customer_id", "customers"),
			},
		},
		"the public schema is no domain": {
			entities: []m.Entity{customers, tickets("public", col("customer_id", m.TypeInt))},
		},
		"the reference records its mapping": {
			entities: []m.Entity{
				customers,
				tickets("support", fieldWith("customer_id", m.TypeInt,
					"mapping", "docs/adr/0007-customer.md")),
			},
		},
		"a declared domain overrides the schema": {
			entities: []m.Entity{
				customers,
				func() m.Entity {
					e := tickets("support", col("customer_id", m.TypeInt))
					e.Annotations = ann("domain", "crm")
					return e
				}(),
			},
		},
		"a mapping table crosses on purpose": {
			entities: []m.Entity{
				customers,
				inSchema("support", "users", []string{"user_id"}, col("user_id", m.TypeInt)),
				referencing(new(referencing(
					new(inSchema("support", "customer_user_xref", nil,
						col("customer_id", m.TypeInt), col("user_id", m.TypeInt))),
					"customer_id", "crm.customers")), "user_id", "support.users"),
			},
		},
		"an entity declared as a translation": {
			entities: []m.Entity{
				customers,
				func() m.Entity {
					e := tickets("support", col("customer_id", m.TypeInt))
					e.Annotations = ann("translates", "from=support to=crm")
					return e
				}(),
			},
		},
		"a shared taxonomy is exempt": {
			entities: []m.Entity{
				regions,
				referencing(new(inSchema("sales", "stores", []string{"store_id"},
					col("store_id", m.TypeInt), col("region_id", m.TypeInt))),
					"region_id", "geo.regions"),
			},
		},
		"an inferred reference is not a declaration": {
			entities: []m.Entity{
				customers,
				func() m.Entity {
					e := tickets("support", col("customer_id", m.TypeInt))
					e.Refs[0].Inferred = true
					return e
				}(),
			},
		},
		"a Go struct referencing another package": {
			entities: []m.Entity{
				goStruct("crm", "Customer", col("ID", m.TypeInt)),
				referencing(new(goStruct("support", "Ticket", col("Customer", m.TypeObject))),
					"Customer", "Customer"),
			},
			want: []string{"2.2-A Ticket Customer"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := &m.Input{Sources: []m.Source{{Entities: tc.entities}}}
			equals(t, tc.want, findingsOf(in, "2.2-A"))
		})
	}
}

// TestBoundaryFollowsDomains checks 2.2-A against its definition for any pair
// of schemas: a reference is reported exactly when both sides have a domain
// and the domains differ, and declaring each table's structural domain
// explicitly changes nothing.
func TestBoundaryFollowsDomains(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		schemas := rapid.SampledFrom([]string{"public", "crm", "sales", "support"})
		from, to := schemas.Draw(t, "from"), schemas.Draw(t, "to")
		declare := rapid.Bool().Draw(t, "declare")
		customers := inSchema(to, "customers", []string{"customer_id"},
			col("customer_id", m.TypeInt))
		tickets := referencing(new(inSchema(from, "tickets", []string{"ticket_id"},
			col("ticket_id", m.TypeInt), col("customer_id", m.TypeInt))),
			"customer_id", to+".customers")
		if declare && from != "public" && to != "public" {
			customers.Annotations = ann("domain", to)
			tickets.Annotations = ann("domain", from)
		}
		got := findingsOf(src(customers, tickets), "2.2-A")
		crosses := from != to && from != "public" && to != "public"
		if crosses != (len(got) == 1) {
			t.Fatalf("schemas %s -> %s (declared %v): got %v", from, to, declare, got)
		}
	})
}

func TestFreeTextClassification(t *testing.T) {
	t.Parallel()
	products := func(fields ...m.Field) m.Entity {
		return table("products", []string{"product_id"},
			append([]m.Field{col("product_id", m.TypeInt)}, fields...)...)
	}
	categories := entityOf("categories", []string{"taxonomy", "product"},
		col("category_id", m.TypeInt), col("category_name", m.TypeString))
	categories.PrimaryKey = []string{"category_id"}
	cases := map[string]struct {
		entities []m.Entity
		want     []string
	}{
		"free-text category": {
			entities: []m.Entity{products(col("category", m.TypeString))},
			want:     []string{"2.3-A products category"},
		},
		"a Go field in PascalCase": {
			entities: []m.Entity{products(col("SalesRegion", m.TypeString))},
			want:     []string{"2.3-A products SalesRegion"},
		},
		"constrained by an enum": {
			entities: []m.Entity{products(m.Field{
				Name: "tier", Type: m.TypeString, Enum: []string{"gold", "silver"},
			})},
		},
		"constrained by a CHECK": {
			entities: []m.Entity{func() m.Entity {
				e := products(col("segment", m.TypeString))
				e.Checks = []m.Check{{Field: "segment", Enum: []string{"smb", "enterprise"}}}
				return e
			}()},
		},
		"a reference into the taxonomy": {
			entities: []m.Entity{
				categories,
				referencing(new(products(col("category_code", m.TypeString))),
					"category_code", "categories"),
			},
		},
		"a numeric measurement": {
			entities: []m.Entity{products(col("floor_area", m.TypeDecimal))},
		},
		"a word that only contains a term": {
			entities: []m.Entity{products(col("classroom", m.TypeString))},
		},
		"the taxonomy's own label": {entities: []m.Entity{categories}},
		"a third-party schema": {
			entities: []m.Entity{func() m.Entity {
				e := products(col("category", m.TypeString))
				e.Annotations = ann("source", "third_party")
				return e
			}()},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.want, findingsOf(src(tc.entities...), "2.3-A"))
		})
	}
}

func TestMetadataCoverage(t *testing.T) {
	t.Parallel()
	described := func(name string, lt m.LogicalType, doc string, kv ...string) m.Field {
		f := fieldWith(name, lt, kv...)
		f.Doc = doc
		return f
	}
	complete := []m.Field{
		described("order_id", m.TypeInt, "Identifies the order across every system."),
		described("total", m.TypeDecimal, "What the customer paid, tax included.",
			"currency", "USD"),
		described("placed_at", m.TypeTimestamptz, "When the customer confirmed checkout."),
	}
	orders := func(kv []string, fields ...m.Field) m.Entity {
		e := entityOf("orders", kv, fields...)
		e.PrimaryKey = []string{"order_id"}
		return e
	}
	validated := []string{"status", "validated"}
	cases := map[string]struct {
		entities []m.Entity
		want     []string
	}{
		"complete": {entities: []m.Entity{orders(validated, complete...)}},
		"no status": {
			entities: []m.Entity{orders(nil, complete...)},
			want:     []string{"2.5-A orders"},
		},
		"a status outside the vocabulary": {
			entities: []m.Entity{orders([]string{"status", "done"}, complete...)},
			want:     []string{"2.5-A orders"},
		},
		"undescribed columns": {
			entities: []m.Entity{orders(validated, complete[0],
				described("total", m.TypeDecimal, "", "currency", "USD"), complete[2])},
			want: []string{"2.5-A orders"},
		},
		"a description that restates the name": {
			entities: []m.Entity{orders(validated,
				described("order_id", m.TypeInt, "Order ID, order ID."), complete[1], complete[2])},
			want: []string{"2.5-A orders"},
		},
		"annotations alone are no description": {
			entities: []m.Entity{orders(validated, complete[0],
				described("total", m.TypeDecimal, "@currency: USD", "currency", "USD"),
				complete[2])},
			want: []string{"2.5-A orders"},
		},
		"a measure without a unit": {
			entities: []m.Entity{orders(validated, complete[0],
				described("total", m.TypeDecimal, "What the customer paid, tax included."),
				complete[2])},
			want: []string{"2.5-A orders"},
		},
		"a measure-like key is no measure": {
			entities: []m.Entity{orders(validated, append(
				slices.Clone(complete),
				described(
					"cost_center_id",
					m.TypeInt,
					"The cost center that books the sale.",
				),
			)...)},
		},
		"published scratch is still scratch": {
			entities: []m.Entity{orders([]string{"lifecycle", "scratch"}, complete...)},
		},
		"recurring is not yet published": {
			entities: []m.Entity{orders([]string{"lifecycle", "recurring"}, complete...)},
		},
		"published": {
			entities: []m.Entity{orders([]string{"lifecycle", "published"}, complete...)},
			want:     []string{"2.5-A orders"},
		},
		"a third-party schema": {
			entities: []m.Entity{orders([]string{"source", "third_party"}, complete...)},
		},
		"a view": {
			entities: []m.Entity{func() m.Entity {
				e := orders(nil, complete...)
				e.Kind = m.KindView
				return e
			}()},
		},
		"another form describes the columns": {
			entities: []m.Entity{
				orders(validated, col("order_id", m.TypeInt),
					fieldWith("total", m.TypeDecimal, "currency", "USD"),
					col("placed_at", m.TypeTimestamptz)),
				{
					ID: "go:example.com/model.Order", Kind: m.KindStruct, Source: m.SourceGo,
					Name: "Order", Fields: complete,
					Annotations: m.Annotations{},
				},
			},
		},
		"one finding for all forms": {
			entities: []m.Entity{
				orders(nil, complete...),
				{
					ID: "go:example.com/model.Order", Kind: m.KindStruct, Source: m.SourceGo,
					Name: "Order", Fields: complete, Annotations: m.Annotations{},
				},
			},
			want: []string{"2.5-A orders"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.want, findingsOf(src(tc.entities...), "2.5-A"))
		})
	}
}
