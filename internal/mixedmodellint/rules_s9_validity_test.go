package mixedmodellint_test

import (
	"slices"
	"testing"

	"pgregory.net/rapid"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// prices is a product_prices table with the given validity columns; ordered
// adds CHECK (valid_to > valid_from).
func prices(name string, ordered bool, fields ...m.Field) m.Entity {
	e := table(name, []string{"price_id"}, append([]m.Field{
		col("price_id", m.TypeInt), col("product_id", m.TypeInt), col("amount", m.TypeDecimal),
	}, fields...)...)
	if ordered {
		e.Checks = []m.Check{{Columns: []string{"valid_to", "valid_from"}}}
	}
	return e
}

func TestLifespanFacts(t *testing.T) {
	t.Parallel()
	from, to := col("valid_from", m.TypeDate), nullableCol("valid_to", m.TypeDate)
	sentinel := col("valid_to", m.TypeDate)
	sentinel.Default = "9999-12-31"
	span := m.Field{Name: "valid_during", RawType: "tstzrange"}
	plan := jsonDoc("plan.schema.json", "plan", "", from, to)
	declared := entityOf("memberships", []string{"lifespan", "true"},
		col("member_id", m.TypeInt))
	rates := table("rates", []string{"rate_code", "effective_from"},
		col("rate_code", m.TypeString), col("effective_from", m.TypeDate))
	cases := map[string]struct {
		entities []m.Entity
		want     []string
	}{
		"a price with no validity": {
			entities: []m.Entity{prices("product_prices", false)},
			want:     []string{"9.5-A product_prices"},
		},
		"a complete pair": {
			entities: []m.Entity{prices("product_prices", true, from, to)},
		},
		"one range column":           {entities: []m.Entity{prices("product_prices", false, span)}},
		"a JSON pair needs no CHECK": {entities: []m.Entity{plan}},
		"no ordering check": {
			entities: []m.Entity{prices("product_prices", false, from, to)},
			want:     []string{"9.5-A product_prices"},
		},
		"a nullable start": {
			entities: []m.Entity{prices("product_prices", true,
				nullableCol("valid_from", m.TypeDate), to)},
			want: []string{"9.5-A product_prices"},
		},
		"ends of two types": {
			entities: []m.Entity{prices("product_prices", true, from,
				nullableCol("valid_to", m.TypeTimestamptz))},
			want: []string{"9.5-A product_prices"},
		},
		"a dimension is SCD1": {entities: []m.Entity{prices("dim_price", false)}},
		"lifespan declared": {
			entities: []m.Entity{declared},
			want:     []string{"9.5-A memberships"},
		},
		"keyed by its start": {entities: []m.Entity{rates}, want: []string{"9.5-A rates"}},
		"no lifespan declared": {entities: []m.Entity{func() m.Entity {
			e := prices("product_prices", false)
			e.Annotations = ann("lifespan", "false")
			return e
		}()}},
		"a word inside another": {entities: []m.Entity{prices("explanations", false)}},
		"open ends marked two ways": {
			entities: []m.Entity{
				prices("product_prices", true, from, to),
				prices("customer_addresses", true, from, to),
				prices("price_tiers", true, from, sentinel),
			},
			want: []string{"9.5-A price_tiers valid_to"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.want, findingsOf(src(tc.entities...), "9.5-A"))
		})
	}
}

// TestLifespanFieldOrder checks that the order of an entity's fields never
// changes what §9.5-A finds.
func TestLifespanFieldOrder(t *testing.T) {
	t.Parallel()
	base := prices("product_prices", false, col("valid_from", m.TypeDate),
		nullableCol("valid_to", m.TypeTimestamptz))
	want := findingsOf(src(base), "9.5-A")
	rapid.Check(t, func(t *rapid.T) {
		e := base
		e.Fields = rapid.Permutation(base.Fields).Draw(t, "fields")
		if got := findingsOf(src(e), "9.5-A"); !slices.Equal(want, got) {
			t.Fatalf("expected %v, got %v", want, got)
		}
	})
}

func TestBitemporalShape(t *testing.T) {
	t.Parallel()
	bitemporal := []string{"audit", "bitemporal"}
	full := []m.Field{
		col("ticket_id", m.TypeInt), col("valid_from", m.TypeTimestamptz),
		nullableCol("valid_to", m.TypeTimestamptz), col("txn_from", m.TypeTimestamptz),
		nullableCol("txn_to", m.TypeTimestamptz),
	}
	keyed := func(pk []string, fields ...m.Field) m.Entity {
		e := entityOf("tickets", bitemporal, fields...)
		e.PrimaryKey = pk
		return e
	}
	cases := map[string]struct {
		entity m.Entity
		want   []string
	}{
		"complete": {entity: keyed([]string{"ticket_id", "txn_from"}, full...)},
		"no transaction time": {
			entity: keyed([]string{"ticket_id"}, full[:3]...),
			want:   []string{"9.6-A tickets"},
		},
		"keyed by the business key alone": {
			entity: keyed([]string{"ticket_id"}, full...),
			want:   []string{"9.6-A tickets"},
		},
		"two range columns": {
			entity: keyed([]string{"ticket_id", "system_period"}, col("ticket_id", m.TypeInt),
				m.Field{Name: "valid_period", RawType: "tstzrange"},
				m.Field{Name: "system_period", RawType: "tstzrange"}),
		},
		"undeclared": {entity: table("tickets", []string{"ticket_id"}, full[:1]...)},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.want, findingsOf(src(tc.entity), "9.6-A"))
		})
	}
}
