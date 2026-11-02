package mixedmodellint_test

import (
	"testing"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

func TestDeclaredPrecision(t *testing.T) {
	t.Parallel()
	ts := func(name, raw string, kv ...string) m.Field {
		f := fieldWith(name, m.TypeTimestamptz, kv...)
		f.RawType = raw
		return f
	}
	cases := map[string]struct {
		f    m.Field
		want []string
	}{
		"default precision": {f: ts("clicked_at", "timestamptz")},
		"whole seconds": {
			f:    ts("clicked_at", "timestamptz(0)"),
			want: []string{"9.8-A clicks clicked_at"},
		},
		"milliseconds": {
			f:    ts("clicked_at", "timestamp(3)"),
			want: []string{"9.8-A clicks clicked_at"},
		},
		"six digits": {f: ts("clicked_at", "timestamptz(6)")},
		"seconds from a second source": {
			f: ts("clicked_at", "timestamptz(0)", "precision", "s"),
		},
		"milliseconds kept for ms": {
			f: ts("clicked_at", "timestamptz(3)", "precision", "ms"),
		},
		"microseconds lose ns": {
			f:    ts("clicked_at", "timestamptz(6)", "precision", "ns"),
			want: []string{"9.8-A clicks clicked_at"},
		},
		"ingestion time is not checked": {f: ts("loaded_at", "timestamptz(0)")},
		"a date named as an instant": {
			f:    m.Field{Name: "clicked_at", Type: m.TypeDate, RawType: "date"},
			want: []string{"9.8-A clicks clicked_at"},
		},
		"a business date": {f: m.Field{Name: "click_date", Type: m.TypeDate}},
		"a JSON date named as an instant": {
			f:    m.Field{Name: "shipped_at", Type: m.TypeDate, RawType: "string (format date)"},
			want: []string{"9.8-A clicks shipped_at"},
		},
		"text is not a timestamp": {
			f: m.Field{Name: "clicked_at", Type: m.TypeString, RawType: "timestamptz(0)"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := &m.Input{Sources: []m.Source{{Entities: []m.Entity{table("clicks", nil, tc.f)}}}}
			equals(t, tc.want, findingsOf(in, "9.8-A"))
		})
	}
}

func TestCopiedAttributes(t *testing.T) {
	t.Parallel()
	customers := table(
		"customers",
		[]string{"customer_id"},
		col("customer_id", m.TypeInt),
		col("email", m.TypeString),
		col("tier", m.TypeString),
	)
	host := func(name string, kv []string, fields ...m.Field) m.Entity {
		e := entityOf(name, kv, append([]m.Field{col("customer_id", m.TypeInt)}, fields...)...)
		e.Refs = []m.Ref{{Fields: []string{"customer_id"}, Target: "customers"}}
		return e
	}
	reason := func(r string) m.Field { return fieldWith("email", m.TypeString, "denormalized_for", r) }
	cases := map[string]struct {
		hosts []m.Entity
		want  []string
	}{
		"same name and type": {
			hosts: []m.Entity{host("orders", nil, col("email", m.TypeString))},
			want:  []string{"3.2-A orders email"},
		},
		"prefixed with the source": {
			hosts: []m.Entity{host("orders", nil, col("customer_tier", m.TypeString))},
			want:  []string{"3.2-A orders customer_tier"},
		},
		"same name, other type": {
			hosts: []m.Entity{host("orders", nil, col("email", m.TypeInt))},
		},
		"two hosts, one finding": {
			hosts: []m.Entity{
				host("orders", nil, col("email", m.TypeString)),
				host("invoices", nil, col("email", m.TypeString)),
			},
			want: []string{"3.2-A orders email"},
		},
		"a stated reason": {
			hosts: []m.Entity{
				host("orders", nil, reason("latency_sla: fraud check answers in 50 ms")),
			},
		},
		"an access pattern": {
			hosts: []m.Entity{
				host("orders", nil, reason("access_pattern: the order page reads it")),
			},
		},
		"another reason": {
			hosts: []m.Entity{host("orders", nil, reason("other: vendor export needs it inline"))},
		},
		"a reason too short": {
			hosts: []m.Entity{host("orders", nil, reason("latency_sla: fast"))},
			want:  []string{"3.2-A orders email"},
		},
		"a reason of no kind": {
			hosts: []m.Entity{host("orders", nil, reason("because the page needs it"))},
			want:  []string{"3.2-A orders email"},
		},
		"a kind without a colon": {
			hosts: []m.Entity{host("orders", nil, reason("other"))},
			want:  []string{"3.2-A orders email"},
		},
		"an event table": {
			hosts: []m.Entity{
				host("order_events", []string{"layer", "event"}, col("email", m.TypeString)),
			},
		},
		"an oltp table": {
			hosts: []m.Entity{
				host("orders", []string{"layer", "oltp"}, col("email", m.TypeString)),
			},
			want: []string{"3.2-A orders email"},
		},
		"a snapshot": {
			hosts: []m.Entity{
				host(
					"orders",
					nil,
					fieldWith("email", m.TypeString, "captured_at_transaction", "true"),
				),
			},
		},
		"a price on a line": {hosts: []m.Entity{
			table(
				"products",
				[]string{"product_id"},
				col("product_id", m.TypeInt),
				col("unit_price", m.TypeDecimal),
			),
			func() m.Entity {
				e := table(
					"order_lines",
					nil,
					col("product_id", m.TypeInt),
					col("unit_price", m.TypeDecimal),
				)
				e.Refs = []m.Ref{{Fields: []string{"product_id"}, Target: "products"}}
				return e
			}(),
		}},
		"bookkeeping names": {hosts: []m.Entity{host("orders", nil, col("status", m.TypeString))}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := &m.Input{
				Sources: []m.Source{{Entities: append([]m.Entity{customers}, tc.hosts...)}},
			}
			equals(t, tc.want, findingsOf(in, "3.2-A"))
		})
	}
}

func TestMasterData(t *testing.T) {
	t.Parallel()
	items := func(kv []string, fields ...m.Field) m.Entity {
		e := entityOf(
			"items",
			append([]string{"layer", "reference"}, kv...),
			append([]m.Field{col("item_id", m.TypeInt)}, fields...)...)
		e.PrimaryKey = []string{"item_id"}
		return e
	}
	nullable := func(f m.Field) m.Field { f.Nullable = true; return f }
	checked := items(nil, col("pack_qty", m.TypeInt))
	checked.Checks = []m.Check{{Field: "pack_qty", Expr: "pack_qty > 0"}}
	unitRef := items(nil, col("base_uom", m.TypeString))
	unitRef.Refs = []m.Ref{{Fields: []string{"base_uom"}, Target: "units"}}
	parent := items(nil, col("parent_id", m.TypeInt))
	parent.Refs = []m.Ref{{Fields: []string{"parent_id"}, Target: "items"}}
	goItems := items(nil, col("pack_qty", m.TypeInt))
	goItems.Source = m.SourceGo
	cases := map[string]struct {
		entities []m.Entity
		want     []string
	}{
		"required but nullable": {
			entities: []m.Entity{
				items([]string{"required", "name"}, nullable(col("name", m.TypeString))),
			},
			want: []string{"3.4-A items name"},
		},
		"required and not null": {
			entities: []m.Entity{items([]string{"required", "name"}, col("name", m.TypeString))},
		},
		"a quantity unchecked": {
			entities: []m.Entity{items(nil, col("pack_qty", m.TypeInt))},
			want:     []string{"3.4-A items pack_qty"},
		},
		"a quantity range-checked": {entities: []m.Entity{checked}},
		"a unit unconstrained": {
			entities: []m.Entity{items(nil, col("base_uom", m.TypeString))},
			want:     []string{"3.4-A items base_uom"},
		},
		"a unit referencing units": {entities: []m.Entity{unitRef}},
		"a unit enumerated": {
			entities: []m.Entity{
				items(
					nil,
					m.Field{Name: "unit_of_measure", Type: m.TypeString, Enum: []string{"kg"}},
				),
			},
		},
		"a level unconstrained": {
			entities: []m.Entity{items(nil, col("level", m.TypeInt))},
			want:     []string{"3.4-A items level"},
		},
		"a parent without a key": {
			entities: []m.Entity{items(nil, col("parent_id", m.TypeInt))},
			want:     []string{"3.4-A items parent_id"},
		},
		"a parent with a key": {entities: []m.Entity{parent}},
		"declared master": {entities: []m.Entity{func() m.Entity {
			e := items(nil, col("pack_qty", m.TypeInt))
			e.Annotations = ann("master", "true")
			return e
		}()}, want: []string{"3.4-A items pack_qty"}},
		"not master data": {
			entities: []m.Entity{
				func() m.Entity { e := items(nil, col("pack_qty", m.TypeInt)); e.Annotations = ann(); return e }(),
			},
		},
		"Go structs cannot say": {entities: []m.Entity{goItems}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := &m.Input{Sources: []m.Source{{Entities: tc.entities}}}
			equals(t, tc.want, findingsOf(in, "3.4-A"))
		})
	}
}

func TestMasterCandidates(t *testing.T) {
	t.Parallel()
	referrers := func(target string, n int) []m.Entity {
		var out []m.Entity
		for i := range n {
			e := table(string(rune('a'+i))+"_uses", nil, col("x", m.TypeInt))
			e.Refs = []m.Ref{{Fields: []string{"x"}, Target: target}}
			out = append(out, e)
		}
		return out
	}
	products := table("products", []string{"product_id"}, col("product_id", m.TypeInt))
	declaredNot := products
	declaredNot.Annotations = ann("master", "false")
	feeds := products
	feeds.Refs = []m.Ref{{Fields: []string{"product_id"}, Target: "events"}}
	events := entityOf("events", []string{"layer", "event"}, col("event_id", m.TypeInt))
	cases := map[string]struct {
		entities []m.Entity
		want     []string
	}{
		"five referrers": {
			entities: append([]m.Entity{products}, referrers("products", 5)...),
			want:     []string{"3.4-A products"},
		},
		"four referrers": {
			entities: append([]m.Entity{products}, referrers("products", 4)...),
		},
		"declared not master": {
			entities: append([]m.Entity{declaredNot}, referrers("products", 5)...),
		},
		"named like something else": {
			entities: append(
				[]m.Entity{table("people", nil, col("p", m.TypeInt))},
				referrers("people", 5)...),
		},
		"referencing event data": {
			entities: append([]m.Entity{feeds, events}, referrers("products", 5)...),
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := &m.Input{Sources: []m.Source{{Entities: tc.entities}}}
			equals(t, tc.want, findingsOf(in, "3.4-A"))
		})
	}
}

func TestAncestorNamedFields(t *testing.T) {
	t.Parallel()
	orders := table(
		"orders",
		[]string{"order_id"},
		col("order_id", m.TypeInt),
		col("shipping_fee", m.TypeDecimal),
		col("order_date", m.TypeDate),
	)
	lines := func(fields ...m.Field) m.Entity {
		e := table(
			"order_lines",
			[]string{"line_id"},
			append([]m.Field{col("line_id", m.TypeInt), col("order_id", m.TypeInt)}, fields...)...)
		e.Refs = []m.Ref{{Fields: []string{"order_id"}, Target: "orders"}}
		return e
	}
	cases := map[string]struct {
		e       m.Entity
		queries []m.Query
		want    []string
		sev     m.Severity
	}{
		"named after the parent": {
			e:    lines(col("order_shipping_fee", m.TypeDecimal)),
			want: []string{"7.4-A order_lines order_shipping_fee"},
			sev:  m.SeverityNote,
		},
		"plural parent prefix": {
			e:    lines(col("orders_note", m.TypeString)),
			want: []string{"7.4-A order_lines orders_note"},
			sev:  m.SeverityNote,
		},
		"named after itself": {e: lines(col("order_line_total", m.TypeDecimal))},
		"a plain total":      {e: lines(col("total", m.TypeDecimal))},
		"declared denormalized": {
			e: lines(
				fieldWith(
					"order_shipping_fee",
					m.TypeDecimal,
					"denormalized_for",
					"other: shown on every line",
				),
			),
		},
		"a lifetime value": {
			e:    lines(col("customer_lifetime_value", m.TypeDecimal)),
			want: []string{"7.4-A order_lines customer_lifetime_value"},
			sev:  m.SeverityNote,
		},
		"the parent has it too": {
			e:    lines(col("order_date", m.TypeDate)),
			want: []string{"7.4-A order_lines order_date"},
			sev:  m.SeverityWarning,
		},
		"a prefix the parent lacks": {
			e:    lines(col("order_status", m.TypeString)),
			want: []string{"7.4-A order_lines order_status"},
			sev:  m.SeverityNote,
		},
		"a write copies it": {
			e: lines(col("order_fee", m.TypeDecimal)),
			queries: []m.Query{
				insert("order_lines", "order_fee", 3, copyOf("orders", "shipping_fee")),
			},
			want: []string{"7.4-A order_lines order_fee"},
			sev:  m.SeverityWarning,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := &m.Input{
				Sources: []m.Source{{Entities: []m.Entity{orders, tc.e}, Queries: tc.queries}},
			}
			equals(t, tc.want, findingsOf(in, "7.4-A"))
			for _, f := range m.Analyze(in).Findings {
				if f.Design == "7.4-A" {
					equals(t, tc.sev, f.Severity)
				}
			}
		})
	}
}

func TestFinerThanTheDeclaredGrain(t *testing.T) {
	t.Parallel()
	readings := table("readings", []string{"sensor_id", "read_hour"}, col("sensor_id", m.TypeInt),
		col("read_hour", m.TypeTimestamptz), col("noted_at", m.TypeTimestamptz))
	readings.Annotations = ann("grain", "one row per sensor per hour")
	ask := func(unit, column string) m.Query {
		return m.Query{
			Kind:      m.QuerySelect,
			Pos:       at(4),
			Relations: []m.Relation{{Name: "readings", Alias: "r"}},
			TimeExprs: []m.TimeExpr{
				{
					Func:   "date_trunc",
					Unit:   unit,
					Pos:    at(4),
					Column: m.ColumnRef{Relation: "r", Column: column},
				},
			},
		}
	}
	cases := map[string]struct {
		q    m.Query
		want []string
	}{
		"minutes from an hourly grain": {
			q:    ask("minute", "read_hour"),
			want: []string{"7.5-D readings read_hour"},
		},
		"the grain's own unit":       {q: ask("hour", "read_hour")},
		"a column outside the grain": {q: ask("minute", "noted_at")},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := &m.Input{
				Sources: []m.Source{{Entities: []m.Entity{readings}, Queries: []m.Query{tc.q}}},
			}
			equals(t, tc.want, findingsOf(in, "7.5-D"))
		})
	}
}

func TestReservedWordLists(t *testing.T) {
	t.Parallel()
	sev := map[string]m.Severity{}
	r := m.Analyze(&m.Input{Sources: []m.Source{{Entities: []m.Entity{
		table(
			"notes",
			nil,
			col(
				"user",
				m.TypeString,
			),
			col("year", m.TypeInt),
			col("value", m.TypeString),
			col("status", m.TypeString),
		),
	}}}})
	for i := range r.Findings {
		if f := &r.Findings[i]; f.Design == "5.1-A" {
			sev[f.Field] = f.Severity
		}
	}
	equals(t, map[string]m.Severity{
		"user":  m.SeverityError,   // reserved in PostgreSQL
		"year":  m.SeverityWarning, // reserved only in SQL:2016
		"value": m.SeverityWarning, // generic, which also covers its SQL:2016 reservation
	}, sev)
}
