package mixedmodellint_test

import (
	"testing"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

func TestUnboundedEmbeddings(t *testing.T) {
	t.Parallel()
	doc := func(f m.Field) m.Entity {
		e := table("customer", nil, col("customer_id", m.TypeString), f)
		e.Source = m.SourceJSONSchema
		return e
	}
	cases := map[string]struct {
		entity m.Entity
		want   []string
	}{
		"an unbounded history": {
			entity: doc(col("order_history", m.TypeArray)),
			want:   []string{"6.5-B customer order_history"},
		},
		"a bounded history": {
			entity: doc(fieldWith("order_history", m.TypeArray, "max-items", "20")),
		},
		"a bound above the ceiling": {
			entity: doc(fieldWith("events", m.TypeArray, "max-items", "5000")),
			want:   []string{"6.5-B customer events"},
		},
		"a list not named for growth":     {entity: doc(col("tags", m.TypeArray))},
		"a list of references is 6.1-A's": {entity: doc(col("order_ids", m.TypeArray))},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.want, findingsOf(src(tc.entity), "6.5-B"))
		})
	}
}

func TestPriceSnapshots(t *testing.T) {
	t.Parallel()
	orders := table("orders", []string{"order_id"}, col("order_id", m.TypeInt))
	products := func(fields ...m.Field) m.Entity {
		return table("products", []string{"product_id"}, append([]m.Field{
			col("product_id", m.TypeInt), col("unit_price", m.TypeDecimal),
		}, fields...)...)
	}
	lines := func(fields ...m.Field) m.Entity {
		e := table("order_lines", []string{"order_id", "line_no"}, append([]m.Field{
			col("order_id", m.TypeInt), col("line_no", m.TypeInt),
			col("product_id", m.TypeInt),
		}, fields...)...)
		e = referencing(&e, "order_id", "orders")
		return referencing(&e, "product_id", "products")
	}
	cases := map[string]struct {
		entities []m.Entity
		want     []string
		level    m.Severity
	}{
		"no price on the line": {
			entities: []m.Entity{orders, products(), lines()},
			want:     []string{"6.6-A order_lines unit_price"}, level: m.SeverityNote,
		},
		"a master price with validity columns": {
			entities: []m.Entity{orders, products(col("valid_from", m.TypeDate)), lines()},
			want:     []string{"6.6-A order_lines unit_price"}, level: m.SeverityWarning,
		},
		"the line captures the price": {
			entities: []m.Entity{
				orders,
				products(),
				lines(col("price_at_purchase", m.TypeDecimal)),
			},
		},
		"a header without a master": {
			entities: []m.Entity{orders, func() m.Entity {
				e := table("order_notes", []string{"order_id", "line_no"},
					col("order_id", m.TypeInt), col("line_no", m.TypeInt))
				return referencing(&e, "order_id", "orders")
			}()},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := src(tc.entities...)
			equals(t, tc.want, findingsOf(in, "6.6-A"))
			for _, f := range m.Analyze(in).Findings {
				if f.Design == "6.6-A" {
					equals(t, tc.level, f.Severity)
				}
			}
		})
	}
}

func TestInferredLinks(t *testing.T) {
	t.Parallel()
	full := []m.Field{
		col("confidence", m.TypeDecimal), col("match_method", m.TypeString),
		enumField("review_status", []string{"auto_applied", "pending_review", "confirmed"}),
	}
	matches := func(fields ...m.Field) m.Entity {
		return table("customer_matches", nil, append([]m.Field{
			col("left_id", m.TypeInt), col("right_id", m.TypeInt),
		}, fields...)...)
	}
	cases := map[string]struct {
		entity m.Entity
		want   []string
	}{
		"a complete record": {entity: matches(full...)},
		"no review state": {
			entity: matches(full[0], full[1], enumField("review_status",
				[]string{"confirmed", "rejected"})),
			want: []string{"6.7-A customer_matches"},
		},
		"a nullable confidence": {
			entity: matches(nullableCol("confidence", m.TypeDecimal), full[1], full[2]),
			want:   []string{"6.7-A customer_matches"},
		},
		"nothing recorded": {
			entity: matches(),
			want:   []string{"6.7-A customer_matches"},
		},
		"a score beside two references": {
			entity: func() m.Entity {
				e := table("pairs", nil, col("a_id", m.TypeInt), col("b_id", m.TypeInt),
					col("similarity", m.TypeFloat))
				e.Refs = []m.Ref{
					{Fields: []string{"a_id"}, Target: "docs"},
					{Fields: []string{"b_id"}, Target: "docs"},
				}
				return e
			}(),
			want: []string{"6.7-A pairs"},
		},
		"an ordinary table": {entity: table("orders", nil, col("total", m.TypeDecimal))},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.want, findingsOf(src(tc.entity), "6.7-A"))
		})
	}
}
