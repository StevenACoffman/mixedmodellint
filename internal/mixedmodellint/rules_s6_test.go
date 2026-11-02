package mixedmodellint_test

import (
	"testing"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

func TestReferenceLists(t *testing.T) {
	t.Parallel()
	tags := table("tags", []string{"tag_id"}, col("tag_id", m.TypeInt))
	withRef := func(e m.Entity, field, target string) m.Entity {
		e.Refs = append(e.Refs, m.Ref{Fields: []string{field}, Target: target})
		return e
	}
	posts := func(kv []string, fields ...m.Field) m.Entity {
		e := entityOf("posts", kv, append([]m.Field{col("post_id", m.TypeInt)}, fields...)...)
		e.PrimaryKey = []string{"post_id"}
		return e
	}
	lines := withRef(
		table("lines", []string{"line_id"}, col("line_id", m.TypeInt), col("post_id", m.TypeInt)),
		"post_id",
		"posts",
	)
	cases := map[string]struct {
		entities []m.Entity
		want     []string
	}{
		"array named as IDs": {
			entities: []m.Entity{posts(nil, col("tag_ids", m.TypeArray))},
			want:     []string{"6.1-A posts tag_ids"},
		},
		"array of another entity": {
			entities: []m.Entity{
				tags,
				withRef(posts(nil, col("tags", m.TypeArray)), "tags", "tags"),
			},
			want: []string{"6.1-A posts tags"},
		},
		"has-many with a back reference": {
			entities: []m.Entity{
				lines,
				withRef(posts(nil, col("lines", m.TypeArray)), "lines", "lines"),
			},
		},
		"array of plain values": {entities: []m.Entity{posts(nil, col("labels", m.TypeArray))}},
		"a scalar ID":           {entities: []m.Entity{posts(nil, col("tag_id", m.TypeInt))}},
		"third-party schema": {
			entities: []m.Entity{
				posts([]string{"source", "third_party"}, col("tag_ids", m.TypeArray)),
			},
		},
		"owner-only document": {
			entities: []m.Entity{
				posts([]string{"consumers", "editor"}, col("tag_ids", m.TypeArray)),
			},
		},
		"shared analytical layer": {
			entities: []m.Entity{
				posts(
					[]string{"consumers", "editor", "layer", "analytical"},
					col("tag_ids", m.TypeArray),
				),
			},
			want: []string{"6.1-A posts tag_ids"},
		},
		"several consumers": {
			entities: []m.Entity{
				posts([]string{"consumers", "editor, search"}, col("tag_ids", m.TypeArray)),
			},
			want: []string{"6.1-A posts tag_ids"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := &m.Input{Sources: []m.Source{{Entities: tc.entities}}}
			equals(t, tc.want, findingsOf(in, "6.1-A"))
		})
	}
}

func TestUndeclaredReferences(t *testing.T) {
	t.Parallel()
	customers := table("customers", []string{"customer_id"}, col("customer_id", m.TypeInt))
	orders := func(fields ...m.Field) m.Entity {
		return table(
			"orders",
			[]string{"order_id"},
			append([]m.Field{col("order_id", m.TypeInt)}, fields...)...)
	}
	declared := orders(col("customer_id", m.TypeInt))
	declared.Refs = []m.Ref{{Fields: []string{"customer_id"}, Target: "customers"}}
	inferred := orders(col("customer_id", m.TypeInt))
	inferred.Refs = []m.Ref{{Fields: []string{"customer_id"}, Target: "customers", Inferred: true}}
	goOrder := m.Entity{
		ID: "go:model.Order", Kind: m.KindStruct, Source: m.SourceGo, Name: "Order",
		PrimaryKey: []string{"order_id"},
		Fields:     []m.Field{col("order_id", m.TypeInt), col("customer_id", m.TypeInt)},
	}
	vendor := orders(col("customer_id", m.TypeInt))
	vendor.Annotations = ann("source", "legacy")
	cases := map[string]struct {
		entities []m.Entity
		want     []string
	}{
		"no foreign key": {
			entities: []m.Entity{customers, orders(col("customer_id", m.TypeInt))},
			want:     []string{"6.2-A orders customer_id"},
		},
		"declared foreign key": {entities: []m.Entity{customers, declared}},
		"inferred is not enough": {
			entities: []m.Entity{customers, inferred},
			want:     []string{"6.2-A orders customer_id"},
		},
		"declared on the linked table": {entities: []m.Entity{customers, declared, goOrder}},
		"names nothing": {
			entities: []m.Entity{orders(col("stripe_customer_id", m.TypeString))},
		},
		"polymorphic subject": {
			entities: []m.Entity{
				customers,
				orders(col("customer_id", m.TypeInt), col("customer_type", m.TypeString)),
			},
		},
		"own key": {entities: []m.Entity{orders()}},
		"parent self-reference": {
			entities: []m.Entity{orders(col("parent_id", m.TypeInt))},
			want:     []string{"6.2-A orders parent_id"},
		},
		"legacy schema": {entities: []m.Entity{customers, vendor}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := &m.Input{Sources: []m.Source{{Entities: tc.entities}}}
			equals(t, tc.want, findingsOf(in, "6.2-A"))
		})
	}
}

func TestMandatoryParents(t *testing.T) {
	t.Parallel()
	lines := func(f m.Field, inferred bool) m.Entity {
		e := table("lines", []string{"line_id"}, col("line_id", m.TypeInt), f)
		e.Refs = []m.Ref{{Fields: []string{f.Name}, Target: "orders", Inferred: inferred}}
		return e
	}
	nullable := func(kv ...string) m.Field {
		f := fieldWith("order_id", m.TypeInt, kv...)
		f.Nullable = true
		return f
	}
	cases := map[string]struct {
		e    m.Entity
		want []string
	}{
		"nullable, cardinality undeclared": {
			e:    lines(nullable(), false),
			want: []string{"6.2-B lines order_id"},
		},
		"nullable but mandatory": {
			e:    lines(nullable("card", "1..N"), false),
			want: []string{"6.2-B lines order_id"},
		},
		"declared optional": {e: lines(nullable("card", "0..N"), false)},
		"unreadable cardinality": {
			e:    lines(nullable("card", "some"), false),
			want: []string{"6.2-B lines order_id"},
		},
		"not nullable":       {e: lines(col("order_id", m.TypeInt), false)},
		"inferred reference": {e: lines(nullable(), true)},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := &m.Input{Sources: []m.Source{{Entities: []m.Entity{tc.e}}}}
			equals(t, tc.want, findingsOf(in, "6.2-B"))
		})
	}
}

func TestRefAnnotationDeclaresAReference(t *testing.T) {
	t.Parallel()
	customers := table("customers", []string{"customer_id"}, col("customer_id", m.TypeInt))
	event := table("order_placed", []string{"event_id"}, col("event_id", m.TypeInt),
		fieldWith("customer_id", m.TypeInt, "ref", "customers"))
	guessed := event
	guessed.Refs = []m.Ref{{Fields: []string{"customer_id"}, Target: "customers", Inferred: true}}
	bare := table(
		"order_placed",
		[]string{"event_id"},
		col("event_id", m.TypeInt),
		col("customer_id", m.TypeInt),
	)
	cases := map[string]struct {
		e    m.Entity
		want []string
	}{
		"ref annotation":              {e: event},
		"ref annotation over a guess": {e: guessed},
		"no annotation, no foreign key": {
			e:    bare,
			want: []string{"6.2-A order_placed customer_id"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := &m.Input{Sources: []m.Source{{Entities: []m.Entity{customers, tc.e}}}}
			equals(t, tc.want, findingsOf(in, "6.2-A"))
		})
	}
	// The caller's references are not changed by the declaration.
	equals(t, true, guessed.Refs[0].Inferred)
}
