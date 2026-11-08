package mixedmodellint_test

import (
	"fmt"
	"testing"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// godEntity returns a customers table with n required attributes plus the
// nullable fields extra.
func godEntity(n int, kv []string, extra ...m.Field) m.Entity {
	fields := []m.Field{col("customer_id", m.TypeInt)}
	for i := range n {
		fields = append(fields, col(fmt.Sprintf("trait%d", i), m.TypeString))
	}
	e := entityOf("customers", kv, append(fields, extra...)...)
	e.PrimaryKey = []string{"customer_id"}
	return e
}

func TestGodEntities(t *testing.T) {
	t.Parallel()
	billing := []m.Field{
		nullableCol("billing_street", m.TypeString), nullableCol("billing_city", m.TypeString),
		nullableCol("billing_zip", m.TypeString),
	}
	cases := map[string]struct {
		entity m.Entity
		want   []string
	}{
		"wide with a sparse group": {
			entity: godEntity(27, nil, billing...),
			want:   []string{"4.8-A customers"},
		},
		"wide but every attribute applies to every row": {
			entity: godEntity(40, nil),
		},
		"a sparse group on a narrow entity": {
			entity: godEntity(5, nil, billing...),
		},
		"bookkeeping columns do not count toward the width": {
			entity: godEntity(25, nil, append(billing, col("created_at", m.TypeTimestamptz))...),
		},
		"a required field in the group means it is not a subset": {
			entity: godEntity(27, nil, append(billing, col("billing_country", m.TypeString))...),
		},
		"two nullable fields are not a group": {
			entity: godEntity(28, nil, billing[:2]...),
		},
		"a wide table built for reading": {
			entity: godEntity(27, []string{"layer", "obt"}, billing...),
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.want, findingsOf(src(tc.entity), "4.8-A"))
		})
	}
}

func TestForeignAttributes(t *testing.T) {
	t.Parallel()
	customers := table("customers", []string{"customer_id"}, col("customer_id", m.TypeInt),
		col("shoe_size", m.TypeInt))
	warehouses := table("warehouses", []string{"warehouse_id"}, col("warehouse_id", m.TypeInt))
	orders := func(fields ...m.Field) m.Entity {
		e := table("orders", []string{"order_id"},
			append([]m.Field{col("order_id", m.TypeInt), col("customer_id", m.TypeInt)},
				fields...)...)
		return referencing(&e, "customer_id", "customers")
	}
	cases := map[string]struct {
		entities []m.Entity
		want     []string
		level    m.Severity
	}{
		"named for a referenced entity": {
			entities: []m.Entity{customers, orders(col("customer_loyalty_tier", m.TypeString))},
			want:     []string{"4.8-B orders customer_loyalty_tier"},
		},
		"a copy of the parent's field is 3.2-A's": {
			entities: []m.Entity{customers, orders(col("customer_shoe_size", m.TypeInt))},
		},
		"named for an entity it does not reference": {
			entities: []m.Entity{
				customers, warehouses,
				orders(col("warehouse_manager_last_name", m.TypeString)),
			},
			want: []string{"4.8-B orders warehouse_manager_last_name"},
		},
		"a snapshot at an event": {
			entities: []m.Entity{customers, orders(col("customer_tier_at_order", m.TypeString))},
		},
		"declared denormalized": {
			entities: []m.Entity{customers, orders(fieldWith("customer_tier", m.TypeString,
				"denormalized_from", "customers"))},
		},
		"its own name": {
			entities: []m.Entity{customers, orders(col("order_notes", m.TypeString))},
		},
		"an event table is denormalized on purpose": {
			entities: []m.Entity{customers, func() m.Entity {
				e := orders(col("customer_loyalty_tier", m.TypeString))
				e.Annotations = ann("layer", "event")
				return e
			}()},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.want, findingsOf(src(tc.entities...), "4.8-B"))
		})
	}
}

func TestForeignAttributeSeverity(t *testing.T) {
	t.Parallel()
	customers := table("customers", []string{"customer_id"}, col("customer_id", m.TypeInt))
	orders := table("orders", []string{"order_id"}, col("order_id", m.TypeInt),
		col("customer_id", m.TypeInt), col("customer_loyalty_tier", m.TypeString))
	cases := map[string]struct {
		orders m.Entity
		want   m.Severity
	}{
		"both signals": {
			orders: referencing(&orders, "customer_id", "customers"),
			want:   m.SeverityWarning,
		},
		"the name alone": {orders: orders, want: m.SeverityNote},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := m.Analyze(src(customers, tc.orders))
			for _, f := range r.Findings {
				if f.Design == "4.8-B" {
					equals(t, tc.want, f.Severity)
					return
				}
			}
			t.Fatal("no 4.8-B finding")
		})
	}
}

func TestJunctionDefinitions(t *testing.T) {
	t.Parallel()
	customers := table("customers", []string{"customer_id"}, col("customer_id", m.TypeInt))
	products := table("products", []string{"product_id"}, col("product_id", m.TypeInt))
	link := func(kv ...string) m.Entity {
		e := entityOf("customer_product_xref", kv, col("customer_id", m.TypeInt),
			col("product_id", m.TypeInt))
		e.PrimaryKey = []string{"customer_id", "product_id"}
		e = referencing(&e, "customer_id", "customers")
		return referencing(&e, "product_id", "products")
	}
	cases := map[string]struct {
		junction m.Entity
		want     []string
	}{
		"a definition naming both sides": {
			junction: link("definition", "A customer has saved a product to a wishlist."),
		},
		"a definition naming one side": {
			junction: link("definition", "A customer bookmark kept for marketing follow-up."),
			want:     []string{"4.9-A customer_product_xref"},
		},
		"a definition naming neither side": {
			junction: link("definition", "Rows the old loader wrote; kept for compatibility."),
			want:     []string{"4.9-A customer_product_xref"},
		},
		"a synonym names a side": {
			junction: func() m.Entity {
				return link("definition", "A client has saved a product to a wishlist.")
			}(),
			want: []string{"4.9-A customer_product_xref"},
		},
		"no definition is 1.2-A's": {junction: link()},
		"marked for removal": {
			junction: link("definition", "Unknown.", "removal_candidate", "DATA-12"),
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.want, findingsOf(src(customers, products, tc.junction), "4.9-A"))
		})
	}
	t.Run("a declared synonym counts", func(t *testing.T) {
		t.Parallel()
		clients := customers
		clients.Annotations = ann("synonyms", "client, account holder")
		j := link("definition", "An account holder has saved a product to a wishlist.")
		equals(t, []string(nil), findingsOf(src(clients, products, j), "4.9-A"))
	})
}

func TestSiblingEntities(t *testing.T) {
	t.Parallel()
	tickets := func(name string, fields ...m.Field) m.Entity {
		base := []m.Field{
			col("ticket_id", m.TypeInt), col("subject", m.TypeString),
			col("body", m.TypeString), col("priority", m.TypeString),
			col("opened_at", m.TypeTimestamptz), col("requester_email", m.TypeString),
		}
		return table(name, []string{"ticket_id"}, append(base, fields...)...)
	}
	cases := map[string]struct {
		entities []m.Entity
		want     []string
	}{
		"channel siblings with the same fields": {
			entities: []m.Entity{tickets("chat_tickets"), tickets("email_tickets")},
			want:     []string{"4.10-A chat_tickets"},
		},
		"generic-noun siblings": {
			entities: []m.Entity{
				table("customer_profiles", nil, col("nickname", m.TypeString)),
				table("customer_accounts", nil, col("balance", m.TypeDecimal)),
			},
			want: []string{"4.10-A customer_profiles"},
		},
		"unrelated names and fields": {
			entities: []m.Entity{tickets("chat_tickets"), table("refunds", nil,
				col("amount", m.TypeDecimal))},
		},
		"the same structure under unrelated names": {
			entities: []m.Entity{tickets("support_requests"), tickets("complaints")},
			want:     []string{"4.10-A support_requests"},
		},
		"subtypes keyed by one supertype": {
			entities: []m.Entity{
				table("tickets", []string{"ticket_id"}, col("ticket_id", m.TypeInt)),
				referencing(new(tickets("chat_tickets")), "ticket_id", "tickets"),
				referencing(new(tickets("email_tickets")), "ticket_id", "tickets"),
			},
		},
		"a declared split": {
			entities: []m.Entity{tickets("chat_tickets"), func() m.Entity {
				e := tickets("email_tickets")
				e.Annotations = ann("split_reason", "email tickets hold PII under another policy")
				return e
			}()},
		},
		"one entity in two sources": {
			entities: []m.Entity{tickets("tickets"), func() m.Entity {
				e := tickets("Ticket")
				e.Source = m.SourceGo
				return e
			}()},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.want, findingsOf(src(tc.entities...), "4.10-A"))
		})
	}
}
