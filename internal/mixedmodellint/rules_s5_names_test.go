package mixedmodellint_test

import (
	"slices"
	"strings"
	"testing"

	"pgregory.net/rapid"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// snakeTable returns a SQL table whose columns are snake_case, plus extra.
func snakeTable(name string, extra ...m.Field) m.Entity {
	fields := []m.Field{
		col("order_id", m.TypeInt), col("placed_at", m.TypeTimestamptz),
		col("total_amount", m.TypeDecimal), col("ship_to", m.TypeString),
		col("billing_city", m.TypeString),
	}
	return table(name, []string{"order_id"}, append(fields, extra...)...)
}

func TestNamingConvention(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		entities []m.Entity
		want     []string
	}{
		"one snake_case model": {entities: []m.Entity{snakeTable("orders")}},
		"a camelCase column among snake_case": {
			entities: []m.Entity{snakeTable("orders", col("shippedAt", m.TypeTimestamptz))},
			want:     []string{"5.2-A orders shippedAt"},
		},
		"a SCREAMING column among snake_case": {
			entities: []m.Entity{snakeTable("orders", col("ORDER_NOTE", m.TypeString))},
			want:     []string{"5.2-A orders ORDER_NOTE"},
		},
		"single lowercase words show no style": {
			entities: []m.Entity{snakeTable("orders", col("status", m.TypeString))},
		},
		"too few names for a convention": {
			entities: []m.Entity{table("t", nil, col("a_b", m.TypeInt), col("cD", m.TypeInt))},
		},
		"the same words spelled two ways": {
			entities: []m.Entity{snakeTable("orders", col("PlacedAt", m.TypeTimestamptz))},
			want:     []string{"5.2-A orders PlacedAt"},
		},
		"Go names follow Go": {
			entities: []m.Entity{func() m.Entity {
				e := snakeTable("Order", col("ShippedAt", m.TypeTimestamptz))
				e.Source = m.SourceGo
				return e
			}()},
		},
		"a singular table among plurals": {
			entities: []m.Entity{
				snakeTable("orders"), snakeTable("customers"),
				snakeTable("invoices"), snakeTable("payments"), snakeTable("shipment"),
			},
			want: []string{"5.2-A shipment"},
		},
		"no dominant number": {
			entities: []m.Entity{
				snakeTable("orders"), snakeTable("customers"),
				snakeTable("invoice"), snakeTable("payment"), snakeTable("shipment"),
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.want, findingsOf(src(tc.entities...), "5.2-A"))
		})
	}
}

// TestConventionalNamesPass checks §5.2-A's fix: a name written in the
// model's dominant style is never reported.
func TestConventionalNamesPass(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		words := rapid.SliceOfN(rapid.StringMatching(`[a-z]{2,8}`), 2, 4).Draw(t, "words")
		e := snakeTable("orders", col(strings.Join(words, "_"), m.TypeString))
		if got := findingsOf(src(e), "5.2-A"); len(got) > 0 {
			t.Fatalf("%v gives %v", words, got)
		}
	})
}

func TestCrypticNames(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		entity m.Entity
		want   []string
	}{
		"spelled-out words": {entity: table("orders", nil, col("quantity", m.TypeInt))},
		"an abbreviation": {
			entity: table("orders", nil, col("cust_qty", m.TypeInt)),
			want:   []string{"5.2-B orders cust_qty"},
		},
		"an abbreviation in Go casing": {
			entity: table("Order", nil, col("CustQty", m.TypeInt)),
			want:   []string{"5.2-B Order CustQty"},
		},
		"an abbreviated table name": {
			entity: table("txn_log", nil, col("logged_at", m.TypeTimestamptz)),
			want:   []string{"5.2-B txn_log"},
		},
		"letters and a number": {
			entity: table("orders", nil, col("c_3", m.TypeInt)),
			want:   []string{"5.2-B orders c_3"},
		},
		"a standard abbreviation": {
			entity: table("orders", nil, col("sku", m.TypeString), col("max_usd", m.TypeDecimal)),
		},
		"a word that contains an abbreviation": {
			entity: table("orders", nil, col("dates", m.TypeString), col("catalog", m.TypeString)),
		},
		"scratch work": {
			entity: entityOf("tmp_orders", []string{"lifecycle", "scratch"}, col("qty", m.TypeInt)),
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.want, findingsOf(src(tc.entity), "5.2-B"))
		})
	}
}

func TestCrypticNameSuggestion(t *testing.T) {
	t.Parallel()
	r := m.Analyze(src(table("Order", nil, col("CustQty", m.TypeInt))))
	i := slices.IndexFunc(r.Findings, func(f m.Finding) bool { return f.Design == "5.2-B" })
	if i < 0 || !strings.Contains(r.Findings[i].Suggestion, "CustomerQuantity") {
		t.Fatalf("want a suggestion of CustomerQuantity, got %+v", r.Findings)
	}
}
