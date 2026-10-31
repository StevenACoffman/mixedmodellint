package mixedmodellint_test

import (
	"slices"
	"strings"
	"testing"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// ann builds annotations from alternating keys and values.
func ann(kv ...string) m.Annotations {
	a := m.Annotations{}
	for i := 0; i+1 < len(kv); i += 2 {
		a[kv[i]] = m.Annotation{Value: kv[i+1]}
	}
	return a
}

// designsFor runs Analyze over entities and returns the design IDs reported,
// sorted, with each ID once.
func designsFor(entities ...m.Entity) []string {
	r := m.Analyze(m.Input{Sources: []m.Source{{Entities: entities}}})
	var ids []string
	for i := range r.Findings {
		f := &r.Findings[i]
		ids = append(ids, f.Design)
	}
	slices.Sort(ids)
	return slices.Compact(ids)
}

// orders is a minimal, fully compliant parent table.
func orders() m.Entity {
	return m.Entity{
		ID: "sql:orders", Kind: m.KindTable, Source: m.SourceSQL, Name: "orders",
		Annotations: ann("grain", "one row per order"),
		PrimaryKey:  []string{"order_id"},
		Fields: []m.Field{
			{Name: "order_id", Type: m.TypeInt},
			{Name: "customer_email", Type: m.TypeString},
			{Name: "placed_at", Type: m.TypeTimestamptz},
		},
	}
}

func TestDesigns(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		entities []m.Entity
		want     []string
	}{
		"compliant table reports nothing": {
			entities: []m.Entity{orders()},
			want:     nil,
		},
		"7.1-A missing grain": {
			entities: []m.Entity{{
				ID: "sql:carts", Kind: m.KindTable, Source: m.SourceSQL, Name: "carts",
				PrimaryKey: []string{"cart_id"},
				Fields:     []m.Field{{Name: "cart_id", Type: m.TypeInt}},
			}},
			want: []string{"7.1-A"},
		},
		"7.1-A malformed grain": {
			entities: []m.Entity{{
				ID: "sql:carts", Kind: m.KindTable, Source: m.SourceSQL, Name: "carts",
				Annotations: ann("grain", "carts"),
				PrimaryKey:  []string{"cart_id"},
				Fields:      []m.Field{{Name: "cart_id", Type: m.TypeInt}},
			}},
			want: []string{"7.1-A"},
		},
		"7.1-A grain noun resolves to nothing": {
			entities: []m.Entity{{
				ID: "sql:carts", Kind: m.KindTable, Source: m.SourceSQL, Name: "carts",
				Annotations: ann("grain", "one row per shopper"),
				PrimaryKey:  []string{"cart_id"},
				Fields:      []m.Field{{Name: "cart_id", Type: m.TypeInt}},
			}},
			want: []string{"7.1-A"},
		},
		"7.1-A stale fingerprint": {
			entities: []m.Entity{{
				ID: "sql:carts", Kind: m.KindTable, Source: m.SourceSQL, Name: "carts",
				Annotations: ann("grain", "one row per cart fp=00000000"),
				PrimaryKey:  []string{"cart_id"},
				Fields:      []m.Field{{Name: "cart_id", Type: m.TypeInt}},
			}},
			want: []string{"7.1-A"},
		},
		"7.1-A scratch entity exempt": {
			entities: []m.Entity{{
				ID: "sql:tmp", Kind: m.KindTable, Source: m.SourceSQL, Name: "tmp",
				Annotations: ann("lifecycle", "scratch"),
			}},
			want: nil,
		},
		"7.1 grain declared on the linked table covers the Go struct": {
			entities: []m.Entity{orders(), {
				ID: "go:model.Order", Kind: m.KindStruct, Source: m.SourceGo, Name: "Order",
				Fields: []m.Field{{Name: "order_id", Type: m.TypeInt}},
			}},
			want: nil,
		},
		"7.1-B grain coarser than key": {
			entities: []m.Entity{orders(), {
				ID: "sql:order_lines", Kind: m.KindTable, Source: m.SourceSQL, Name: "order_lines",
				Annotations: ann("grain", "one row per order"),
				PrimaryKey:  []string{"order_id", "line_no"},
				Refs:        []m.Ref{{Fields: []string{"order_id"}, Target: "orders"}},
				Fields: []m.Field{
					{Name: "order_id", Type: m.TypeInt},
					{Name: "line_no", Type: m.TypeInt},
				},
			}},
			want: []string{"7.1-B"},
		},
		"7.1-B own noun maps to the primary key": {
			entities: []m.Entity{orders(), {
				ID: "sql:order_lines", Kind: m.KindTable, Source: m.SourceSQL, Name: "order_lines",
				Annotations: ann("grain", "one row per order line"),
				PrimaryKey:  []string{"order_id", "line_no"},
				Refs:        []m.Ref{{Fields: []string{"order_id"}, Target: "orders"}},
				Fields: []m.Field{
					{Name: "order_id", Type: m.TypeInt},
					{Name: "line_no", Type: m.TypeInt},
				},
			}},
			want: nil,
		},
		"7.1-B grain with no key": {
			entities: []m.Entity{{
				ID: "sql:visits", Kind: m.KindTable, Source: m.SourceSQL, Name: "visits",
				Annotations: ann("grain", "one row per visit"),
				Fields:      []m.Field{{Name: "visit_id", Type: m.TypeInt}},
			}},
			want: []string{"7.1-B"},
		},
		"7.1-B grain by product and day": {
			entities: []m.Entity{{
				ID: "sql:daily_sales", Kind: m.KindTable, Source: m.SourceSQL, Name: "daily_sales",
				Annotations: ann("grain", "one row per product per sale date"),
				PrimaryKey:  []string{"product_id", "sale_date"},
				Fields: []m.Field{
					{Name: "product_id", Type: m.TypeInt},
					{Name: "sale_date", Type: m.TypeDate},
					{Name: "units_sold_total", Type: m.TypeInt},
				},
			}},
			want: nil,
		},
		"7.3-B event table with no dedup key": {
			entities: []m.Entity{{
				ID: "sql:clicks", Kind: m.KindTable, Source: m.SourceSQL, Name: "clicks",
				Annotations: ann("grain", "one row per click", "layer", "event"),
				Fields: []m.Field{
					{Name: "click_id", Type: m.TypeInt},
					{Name: "clicked_at", Type: m.TypeTimestamptz},
				},
			}},
			want: []string{"7.1-B", "7.3-B"},
		},
		"7.3-B event id key passes": {
			entities: []m.Entity{{
				ID: "sql:clicks", Kind: m.KindTable, Source: m.SourceSQL, Name: "clicks",
				Annotations: ann("grain", "one row per click", "layer", "event"),
				PrimaryKey:  []string{"click_id"},
				Fields: []m.Field{
					{Name: "click_id", Type: m.TypeInt},
					{Name: "clicked_at", Type: m.TypeTimestamptz},
				},
			}},
			want: nil,
		},
		"7.4-B nullable grain_level": {
			entities: []m.Entity{
				{
					ID:          "sql:sales_rollup",
					Kind:        m.KindTable,
					Source:      m.SourceSQL,
					Name:        "sales_rollup",
					Annotations: ann("grain", "one row per store per day or per store per month"),
					PrimaryKey:  []string{"store_id", "period_start", "grain_level"},
					Fields: []m.Field{
						{Name: "store_id", Type: m.TypeInt},
						{Name: "period_start", Type: m.TypeDate},
						{Name: "grain_level", Type: m.TypeString, Nullable: true},
					},
				},
			},
			want: []string{"7.1-A", "7.4-B"},
		},
		"7.4-C child repeats a parent attribute": {
			entities: []m.Entity{orders(), {
				ID: "sql:order_lines", Kind: m.KindTable, Source: m.SourceSQL, Name: "order_lines",
				Annotations: ann("grain", "one row per order line"),
				PrimaryKey:  []string{"order_id", "line_no"},
				Refs:        []m.Ref{{Fields: []string{"order_id"}, Target: "orders"}},
				Fields: []m.Field{
					{Name: "order_id", Type: m.TypeInt},
					{Name: "line_no", Type: m.TypeInt},
					{Name: "customer_email", Type: m.TypeString},
				},
			}},
			want: []string{"7.4-C"},
		},
		"7.5-C events stored as daily counts": {
			entities: []m.Entity{{
				ID: "sql:page_view_events", Kind: m.KindTable, Source: m.SourceSQL,
				Name:        "page_view_events",
				Annotations: ann("grain", "one row per page per view date"),
				PrimaryKey:  []string{"page_id", "view_date"},
				Fields: []m.Field{
					{Name: "page_id", Type: m.TypeInt},
					{Name: "view_date", Type: m.TypeDate},
					{Name: "view_count", Type: m.TypeInt},
				},
			}},
			want: []string{"7.5-C"},
		},
		"5.1-A reserved and bare type words": {
			entities: []m.Entity{{
				ID: "sql:notes", Kind: m.KindTable, Source: m.SourceSQL, Name: "notes",
				Annotations: ann("grain", "one row per note"),
				PrimaryKey:  []string{"note_id"},
				Fields: []m.Field{
					{Name: "note_id", Type: m.TypeInt},
					{Name: "user", Type: m.TypeInt},
					{Name: "date", Type: m.TypeDate},
					{Name: "status", Type: m.TypeString},
				},
			}},
			want: []string{"5.1-A"},
		},
		"5.1-A third-party schema exempt": {
			entities: []m.Entity{{
				ID: "sql:vendor", Kind: m.KindTable, Source: m.SourceSQL, Name: "vendor",
				Annotations: ann("grain", "one row per vendor", "source", "third_party"),
				PrimaryKey:  []string{"vendor_id"},
				Fields: []m.Field{
					{Name: "vendor_id", Type: m.TypeInt},
					{Name: "date", Type: m.TypeDate},
				},
			}},
			want: nil,
		},
		"5.5-A numbered siblings": {
			entities: []m.Entity{{
				ID: "sql:contacts", Kind: m.KindTable, Source: m.SourceSQL, Name: "contacts",
				Annotations: ann("grain", "one row per contact"),
				PrimaryKey:  []string{"contact_id"},
				Fields: []m.Field{
					{Name: "contact_id", Type: m.TypeInt},
					{Name: "phone1", Type: m.TypeString},
					{Name: "phone2", Type: m.TypeString},
					{Name: "phone3", Type: m.TypeString},
					{Name: "address_line1", Type: m.TypeString},
					{Name: "address_line2", Type: m.TypeString},
					{Name: "address_line3", Type: m.TypeString},
				},
			}},
			want: []string{"5.5-A"},
		},
		"8.4-A stored average at a coarse grain": {
			entities: []m.Entity{{
				ID: "sql:store_daily", Kind: m.KindTable, Source: m.SourceSQL, Name: "store_daily",
				Annotations: ann("grain", "one row per store per day", "layer", "analytical"),
				PrimaryKey:  []string{"store_id", "day"},
				Fields: []m.Field{
					{Name: "store_id", Type: m.TypeInt},
					{Name: "day", Type: m.TypeDate},
					{Name: "avg_order_value", Type: m.TypeDecimal},
				},
			}},
			want: []string{"8.4-A"},
		},
		"8.4-A components present": {
			entities: []m.Entity{{
				ID: "sql:store_daily", Kind: m.KindTable, Source: m.SourceSQL, Name: "store_daily",
				Annotations: ann("grain", "one row per store per sale date", "layer", "analytical"),
				PrimaryKey:  []string{"store_id", "sale_date"},
				Fields: []m.Field{
					{Name: "store_id", Type: m.TypeInt},
					{Name: "sale_date", Type: m.TypeDate},
					{Name: "avg_order_value", Type: m.TypeDecimal},
					{Name: "revenue_total", Type: m.TypeDecimal},
					{Name: "order_count", Type: m.TypeInt},
				},
			}},
			want: nil,
		},
		"9.2-A naive timestamp and string date": {
			entities: []m.Entity{{
				ID: "sql:tickets", Kind: m.KindTable, Source: m.SourceSQL, Name: "tickets",
				Annotations: ann("grain", "one row per ticket"),
				PrimaryKey:  []string{"ticket_id"},
				Fields: []m.Field{
					{Name: "ticket_id", Type: m.TypeInt},
					{Name: "opened_at", Type: m.TypeTimestampNaive, RawType: "timestamp"},
					{Name: "closed_on", Type: m.TypeString, RawType: "varchar(10)"},
					{Name: "runtime", Type: m.TypeString},
				},
			}},
			want: []string{"9.2-A"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.want, designsFor(tc.entities...))
		})
	}
}

func TestSeverityFollowsStrength(t *testing.T) {
	t.Parallel()
	e := m.Entity{
		ID: "sql:notes", Kind: m.KindTable, Source: m.SourceSQL, Name: "notes",
		Annotations: ann("grain", "one row per note"),
		PrimaryKey:  []string{"note_id"},
		Fields: []m.Field{
			{Name: "note_id", Type: m.TypeInt},
			{Name: "date", Type: m.TypeDate},
			{Name: "kind", Type: m.TypeString},
			{Name: "amount", Type: m.TypeInt},
		},
	}
	r := m.Analyze(m.Input{Sources: []m.Source{{Entities: []m.Entity{e}}}})
	got := map[string]string{}
	for i := range r.Findings {
		f := &r.Findings[i]
		got[f.Field] = f.Severity.String()
	}
	cases := map[string]struct{ field, want string }{
		"ambiguous type word is an error": {"date", "error"},
		"weakly ambiguous word is a note": {"kind", "note"},
		"generic name is a warning":       {"amount", "warning"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.want, got[tc.field])
		})
	}
	equals(t, true, r.HasErrors())
}

func TestWaiversAndBaseline(t *testing.T) {
	t.Parallel()
	carts := func(anns m.Annotations) m.Entity {
		return m.Entity{
			ID: "sql:carts", Kind: m.KindTable, Source: m.SourceSQL, Name: "carts",
			Annotations: anns, PrimaryKey: []string{"cart_id"},
			Fields: []m.Field{{Name: "cart_id", Type: m.TypeInt}},
		}
	}
	cases := map[string]struct {
		entity     m.Entity
		want       []string
		wantWaived int
	}{
		"waiver with reason removes the finding": {
			entity:     carts(ann("waive", `7.1 reason="legacy table being retired"`)),
			want:       nil,
			wantWaived: 1,
		},
		"waiver without reason is itself reported": {
			entity: carts(ann("waive", "7.1")),
			want:   []string{"7.1-A", "mml/waiver-no-reason"},
		},
		"single-quoted reason": {
			entity:     carts(ann("waive", "§7.1 reason='retiring this table'")),
			want:       nil,
			wantWaived: 1,
		},
		"comma-separated targets": {
			entity:     carts(ann("waive", `5.1,7.1-A reason="legacy"`)),
			want:       nil,
			wantWaived: 1,
		},
		"waiver for another rule does not apply": {
			entity: carts(ann("waive", `5.1 reason="legacy"`)),
			want:   []string{"7.1-A"},
		},
		"empty quoted reason is no reason": {
			entity: carts(ann("waive", `7.1 reason=""`)),
			want:   []string{"7.1-A", "mml/waiver-no-reason"},
		},
		"reason with no target": {
			entity: carts(ann("waive", `reason="nothing named"`)),
			want:   []string{"7.1-A", "mml/waiver-no-reason"},
		},
		"unterminated quote keeps its text": {
			entity:     carts(ann("waive", `7.1 reason="dropped next quarter`)),
			want:       nil,
			wantWaived: 1,
		},
		"unknown key is a note": {
			entity: carts(ann("grian", "one row per cart", "grain", "one row per cart")),
			want:   []string{"mml/unknown-key"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := m.Analyze(m.Input{Sources: []m.Source{{Entities: []m.Entity{tc.entity}}}})
			var ids []string
			for i := range r.Findings {
				f := &r.Findings[i]
				ids = append(ids, f.Design)
			}
			slices.Sort(ids)
			equals(t, tc.want, ids)
			equals(t, tc.wantWaived, r.Waived)
		})
	}
}

func TestBaselineRoundTrip(t *testing.T) {
	t.Parallel()
	e := m.Entity{
		ID: "sql:carts", Kind: m.KindTable, Source: m.SourceSQL, Name: "carts",
		PrimaryKey: []string{"cart_id"},
		Fields:     []m.Field{{Name: "cart_id", Type: m.TypeInt}},
	}
	in := m.Input{Sources: []m.Source{{Entities: []m.Entity{e}}}}
	first := m.Analyze(in)
	var buf strings.Builder
	ok(t, m.WriteBaseline(&buf, first))
	b, err := m.ReadBaseline(strings.NewReader(buf.String()))
	ok(t, err)
	in.Baseline = b
	second := m.Analyze(in)
	equals(t, 0, len(second.Findings))
	equals(t, len(first.Findings), second.Baselined)
}

func TestSidecarConflict(t *testing.T) {
	t.Parallel()
	e := orders()
	in := m.Input{
		Sources: []m.Source{{Entities: []m.Entity{e}}},
		Sidecar: m.Sidecar{File: "mml.yaml", Entities: map[string]m.SidecarEntity{
			"orders": {Annotations: ann("grain", "one row per order line", "owner", "team-orders")},
		}},
	}
	r := m.Analyze(in)
	equals(t, 1, len(r.Findings))
	equals(t, "mml/annotation-conflict", r.Findings[0].Design)
	// The sidecar's owner was merged in, and the input was not modified.
	_, hasOwner := e.Annotations.Get("owner")
	equals(t, false, hasOwner)
}
