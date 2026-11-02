package mixedmodellint_test

import (
	"slices"
	"testing"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// TestDesignRegistry pins what the explain command and reports read from
// each design: a unique ID, a title, and the severity its strength implies.
func TestDesignRegistry(t *testing.T) {
	t.Parallel()
	ds := m.Designs()
	seen := map[string]bool{}
	for _, d := range ds {
		if d.Title == "" {
			t.Errorf("%s has no title", d.ID)
		}
		if seen[d.ID] {
			t.Errorf("%s is registered twice", d.ID)
		}
		seen[d.ID] = true
	}
	sev := map[string]m.Severity{}
	for _, d := range ds {
		sev[d.ID] = d.Severity
	}
	equals(t, map[string]m.Severity{
		"1.2-A": m.SeverityError, "3.2-A": m.SeverityWarning, "7.4-A": m.SeverityNote,
		"8.4-A": m.SeverityWarning, "9.8-A": m.SeverityWarning,
	}, map[string]m.Severity{
		"1.2-A": sev["1.2-A"], "3.2-A": sev["3.2-A"], "7.4-A": sev["7.4-A"],
		"8.4-A": sev["8.4-A"], "9.8-A": sev["9.8-A"],
	})
	if !slices.ContainsFunc(ds, func(d m.DesignInfo) bool {
		return d.ID == "7.4-D" && d.Title == "Copy-on-write from the coarser grain"
	}) {
		t.Error("7.4-D's title is not the rule plan's")
	}
}

// TestThresholds pins each numeric boundary a design applies, on both sides.
func TestThresholds(t *testing.T) {
	t.Parallel()
	definition := func(text string) []string {
		e := governedOrders("definition", text, "owner", "team:orders", "escalation", "role:cto")
		return findingsOf(&m.Input{Sources: []m.Source{{Entities: []m.Entity{e}}}}, "1.2-A")
	}
	equals(
		t,
		[]string(nil),
		definition("A buyer asks for goods to be shipped."),
	) // 8 words, 4 content
	equals(t, []string{"1.2-A orders"}, definition("A buyer asks for goods to ship"))   // 7 words
	equals(t, []string{"1.2-A orders"}, definition("A buyer asks for goods to be it.")) // 3 content

	responsibility := func(r string) []string {
		rec := m.OwnershipRecord{
			Owner:            "team:orders",
			Model:            []string{"orders"},
			Responsibilities: []string{r},
			Decisions: []m.Decision{
				{Name: "deprecate", WithoutSignoff: true},
			},
			GrantedBy: "role:cto",
		}
		return findingsOf(&m.Input{
			Sources:   []m.Source{{Entities: []m.Entity{governedOrders("owner", "team:orders")}}},
			Artifacts: m.Artifacts{Ownership: []m.OwnershipRecord{rec}},
		}, "1.13-A")
	}
	equals(t, []string(nil), responsibility("keep data reconciled"))
	equals(t, []string{"1.13-A orders"}, responsibility("keep reconciled"))

	reason := func(r string) []string {
		customers := table(
			"customers",
			[]string{"customer_id"},
			col("customer_id", m.TypeInt),
			col("email", m.TypeString),
		)
		host := table(
			"orders",
			nil,
			col("customer_id", m.TypeInt),
			fieldWith("email", m.TypeString, "denormalized_for", r),
		)
		host.Refs = []m.Ref{{Fields: []string{"customer_id"}, Target: "customers"}}
		return findingsOf(
			&m.Input{Sources: []m.Source{{Entities: []m.Entity{customers, host}}}},
			"3.2-A",
		)
	}
	equals(t, []string(nil), reason("other: shown on invoices"))
	equals(t, []string{"3.2-A orders email"}, reason("other: shown inline"))
}

func TestLineSnapshots(t *testing.T) {
	t.Parallel()
	products := table(
		"products",
		[]string{"product_id"},
		col("product_id", m.TypeInt),
		col("unit_price", m.TypeDecimal),
		col("unit_cost", m.TypeDecimal),
		col("label", m.TypeString),
	)
	host := func(name, field string, typ m.LogicalType) []string {
		e := table(name, nil, col("product_id", m.TypeInt), col(field, typ))
		e.Refs = []m.Ref{{Fields: []string{"product_id"}, Target: "products"}}
		return findingsOf(
			&m.Input{Sources: []m.Source{{Entities: []m.Entity{products, e}}}},
			"3.2-A",
		)
	}
	for _, name := range []string{"order_line", "order_lines", "cart_item", "cart_items"} {
		equals(t, []string(nil), host(name, "unit_price", m.TypeDecimal))
	}
	equals(t, []string(nil), host("order_lines", "unit_cost", m.TypeDecimal))
	equals(t, []string{"3.2-A carts unit_price"}, host("carts", "unit_price", m.TypeDecimal))
	equals(t, []string{"3.2-A order_lines label"}, host("order_lines", "label", m.TypeString))
	equals(
		t,
		[]string(nil),
		host("order_lines", "label", m.TypeUnknown),
	) // an unknown type proves nothing
}

func TestMasterPredicates(t *testing.T) {
	t.Parallel()
	items := func(f m.Field, checks ...m.Check) m.Entity {
		e := entityOf("items", []string{"layer", "reference"}, col("item_id", m.TypeInt), f)
		e.Checks = checks
		return e
	}
	run := func(entities ...m.Entity) []string {
		return findingsOf(&m.Input{Sources: []m.Source{{Entities: entities}}}, "3.4-A")
	}
	equals(
		t,
		[]string{"3.4-A items unit_of_measure"},
		run(items(col("unit_of_measure", m.TypeString))),
	)
	equals(
		t,
		[]string{"3.4-A items pack_qty"},
		run(items(col("pack_qty", m.TypeInt), m.Check{Field: "other", Expr: "other > 0"})),
	)
	equals(
		t,
		[]string{"3.4-A items pack_qty"},
		run(items(col("pack_qty", m.TypeInt), m.Check{Field: "pack_qty", Enum: []string{"1"}})),
	)
	equals(
		t,
		[]string(nil),
		run(items(col("pack_qty", m.TypeInt), m.Check{Field: "pack_qty", Expr: "pack_qty < 99"})),
	)

	referrers := []m.Entity{}
	for _, n := range []string{"a", "b", "c", "d", "e"} {
		r := table(n+"_uses", nil, col("x", m.TypeInt))
		r.Refs = []m.Ref{{Fields: []string{"x"}, Target: "products"}}
		referrers = append(referrers, r)
	}
	oltp := entityOf("carts", []string{"layer", "oltp"}, col("cart_id", m.TypeInt))
	feeds := table(
		"products",
		[]string{"product_id"},
		col("product_id", m.TypeInt),
		col("cart_id", m.TypeInt),
	)
	feeds.Refs = []m.Ref{{Fields: []string{"cart_id"}, Target: "carts"}}
	equals(t, []string(nil), run(append([]m.Entity{feeds, oltp}, referrers...)...))
}

func TestCopyOrder(t *testing.T) {
	t.Parallel()
	customers := table(
		"customers",
		[]string{"customer_id"},
		col("customer_id", m.TypeInt),
		col("email", m.TypeString),
	)
	host := func(name string, kv ...string) m.Entity {
		e := entityOf(name, kv, col("customer_id", m.TypeInt), col("email", m.TypeString))
		e.Refs = []m.Ref{{Fields: []string{"customer_id"}, Target: "customers"}}
		return e
	}
	reasoned := host("invoices")
	reasoned.Fields[1].Annotations = ann("denormalized_for", "other: printed on every invoice")
	equals(
		t,
		[]string{"3.2-A orders email"},
		findingsOf(&m.Input{Sources: []m.Source{{Entities: []m.Entity{
			customers, host("events", "layer", "event"), reasoned, host("orders"),
		}}}}, "3.2-A"),
	)
}

func TestHistoryGaps(t *testing.T) {
	t.Parallel()
	tickets := entityOf(
		"tickets",
		[]string{"history", "required"},
		col("ticket_id", m.TypeInt),
		col("status", m.TypeString),
	)
	child := func(name string, fields ...m.Field) m.Entity {
		e := entityOf(name, nil, append([]m.Field{col("ticket_id", m.TypeInt)}, fields...)...)
		e.Refs = []m.Ref{{Fields: []string{"ticket_id"}, Target: "tickets"}}
		return e
	}
	run := func(entities ...m.Entity) []string {
		return findingsOf(
			&m.Input{Sources: []m.Source{{Entities: append([]m.Entity{tickets}, entities...)}}},
			"3.1-A",
		)
	}
	want := []string{"3.1-A tickets status"}
	equals(
		t,
		want,
		run(
			child(
				"ticket_events",
				col("actor_id", m.TypeInt),
				col("occurred_at", m.TypeTimestamptz),
			),
		),
	)
	equals(
		t,
		want,
		run(
			child(
				"ticket_events",
				col("event_type", m.TypeString),
				col("occurred_at", m.TypeTimestamptz),
			),
		),
	)
	equals(
		t,
		want,
		run(child("ticket_events", col("actor_id", m.TypeInt), col("event_type", m.TypeString))),
	)
	equals(
		t,
		want,
		run(
			child(
				"ticket_notes",
				col("actor_id", m.TypeInt),
				col("event_type", m.TypeString),
				col("occurred_at", m.TypeTimestamptz),
			),
		),
	)
}

func TestGovernedOrder(t *testing.T) {
	t.Parallel()
	scratch := entityOf(
		"drafts",
		[]string{"lifecycle", "scratch", "owner", "team:x"},
		col("draft_id", m.TypeInt),
	)
	vendor := entityOf(
		"feeds",
		[]string{"source", "third_party", "owner", "team:x"},
		col("feed_id", m.TypeInt),
	)
	unowned := governedOrders()
	in := &m.Input{
		Sources: []m.Source{
			{Entities: []m.Entity{scratch, vendor, unowned, governedOrders("owner", "team:x")}},
		},
	}
	equals(t, []string{"1.13-A orders"}, findingsOf(in, "1.13-A"))
	equals(t, []string{"1.2-A orders", "1.2-A orders"}, findingsOf(in, "1.2-A"))
}
