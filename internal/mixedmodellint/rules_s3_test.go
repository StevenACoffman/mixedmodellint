package mixedmodellint_test

import (
	"testing"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

func TestNullMeaning(t *testing.T) {
	t.Parallel()
	nullable := func(kv ...string) m.Field {
		f := fieldWith("shipped_at", m.TypeTimestamptz, kv...)
		f.Nullable = true
		return f
	}
	orders := func(f m.Field) m.Entity {
		e := table(
			"orders",
			[]string{"order_id"},
			col("order_id", m.TypeInt),
			col("order_type", m.TypeString),
			f,
		)
		return e
	}
	view := orders(nullable())
	view.Kind = m.KindView
	cases := map[string]struct {
		e    m.Entity
		want []string
	}{
		"nullable without a meaning": {
			e:    orders(nullable()),
			want: []string{"5.3-A orders shipped_at"},
		},
		"missing": {e: orders(nullable("null", "missing"))},
		"not a meaning": {
			e:    orders(nullable("null", "absent")),
			want: []string{"5.3-A orders shipped_at"},
		},
		"forbidden but nullable": {
			e:    orders(nullable("null", "forbidden")),
			want: []string{"5.3-A orders shipped_at"},
		},
		"forbidden and not null": {
			e: orders(fieldWith("shipped_at", m.TypeTimestamptz, "null", "forbidden")),
		},
		"not applicable, discriminated": {
			e: orders(nullable("null", "not_applicable", "null_when", "order_type = 'digital'")),
		},
		"not applicable, no predicate": {
			e:    orders(nullable("null", "not_applicable")),
			want: []string{"5.3-A orders shipped_at"},
		},
		"not applicable, unknown column": {
			e:    orders(nullable("null", "not_applicable", "null_when", "kind = 'digital'")),
			want: []string{"5.3-A orders shipped_at"},
		},
		"a bare predicate column": {
			e: orders(nullable("null", "not_applicable", "null_when", "order_type")),
		},
		"not nullable, not declared": {e: orders(col("shipped_at", m.TypeTimestamptz))},
		"views are not checked":      {e: view},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := &m.Input{Sources: []m.Source{{Entities: []m.Entity{tc.e}}}}
			equals(t, tc.want, findingsOf(in, "5.3-A"))
		})
	}
}

func TestHistoryRequired(t *testing.T) {
	t.Parallel()
	orders := func(kv ...string) m.Entity {
		e := entityOf("orders", kv, col("order_id", m.TypeInt), col("status", m.TypeString))
		e.PrimaryKey = []string{"order_id"}
		return e
	}
	child := func(name string, kv []string, fields ...m.Field) m.Entity {
		e := entityOf(name, kv, append([]m.Field{col("order_id", m.TypeInt)}, fields...)...)
		e.Refs = []m.Ref{{Fields: []string{"order_id"}, Target: "orders"}}
		return e
	}
	events := child(
		"order_events",
		nil,
		col("actor_id", m.TypeInt),
		col("event_type", m.TypeString),
		col("occurred_at", m.TypeTimestamptz),
	)
	thin := child("order_events", nil, col("event_type", m.TypeString))
	versions := child(
		"order_versions",
		nil,
		col("valid_from", m.TypeTimestamptz),
		col("valid_to", m.TypeTimestamptz),
	)
	declaredLog := child(
		"audit_trail",
		[]string{"history_of", "orders"},
		col("changed_by", m.TypeString),
		col("new_status", m.TypeString),
		col("occurred_at", m.TypeTimestamptz),
	)
	unrelated := child("order_notes", nil, col("note", m.TypeString))
	byStatus := m.Query{
		Kind: m.QueryCreateView, Target: "orders_by_status", Pos: at(1),
		Relations: []m.Relation{{Name: "orders", Alias: "o"}},
		GroupBy:   []m.ColumnRef{{Relation: "o", Column: "status"}},
	}
	cases := map[string]struct {
		entities []m.Entity
		queries  []m.Query
		want     []string
	}{
		"required, none kept": {
			entities: []m.Entity{orders("history", "required")},
			want:     []string{"3.1-A orders status"},
		},
		"required, event entity": {entities: []m.Entity{orders("history", "required"), events}},
		"required, versions": {
			entities: []m.Entity{orders("history", "required"), versions},
		},
		"required, declared log": {
			entities: []m.Entity{orders("history", "required"), declaredLog},
		},
		"required, external stream": {
			entities: []m.Entity{orders("history", "required", "history_in", "order_events"), thin},
		},
		"required, thin events": {
			entities: []m.Entity{orders("history", "required"), thin},
			want:     []string{"3.1-A orders status"},
		},
		"unrelated child": {
			entities: []m.Entity{orders("history", "required"), unrelated},
			want:     []string{"3.1-A orders status"},
		},
		"not required, not analyzed": {entities: []m.Entity{orders()}},
		"analyzed by a view": {
			entities: []m.Entity{orders()},
			queries:  []m.Query{byStatus},
			want:     []string{"3.1-A orders status"},
		},
		"latest only, but analyzed": {
			entities: []m.Entity{orders("history", "latest_only"), events},
			queries:  []m.Query{byStatus},
			want:     []string{"3.1-A orders status"},
		},
		"latest only, not analyzed": {entities: []m.Entity{orders("history", "latest_only")}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := &m.Input{Sources: []m.Source{{Entities: tc.entities, Queries: tc.queries}}}
			equals(t, tc.want, findingsOf(in, "3.1-A"))
		})
	}
}

func TestUpdatedEnumIsAState(t *testing.T) {
	t.Parallel()
	tickets := entityOf("tickets", []string{"history", "required"}, col("ticket_id", m.TypeInt),
		m.Field{Name: "priority", Type: m.TypeString, Enum: []string{"low", "mid", "high"}})
	update := m.Query{
		Kind: m.QueryUpdate, Target: "tickets", Pos: at(2),
		Assignments: []m.Assignment{{Column: "priority", Pos: at(2)}},
	}
	in := &m.Input{Sources: []m.Source{{Entities: []m.Entity{tickets}}}}
	equals(t, []string(nil), findingsOf(in, "3.1-A"))
	in = &m.Input{Sources: []m.Source{{Entities: []m.Entity{tickets}, Queries: []m.Query{update}}}}
	equals(t, []string{"3.1-A tickets priority"}, findingsOf(in, "3.1-A"))
}
