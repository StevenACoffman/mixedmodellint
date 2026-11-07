package mixedmodellint_test

import (
	"testing"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// queryMessageCases covers the query-corpus designs 1.5-D, 5.3-B, 8.3-B,
// 8.4-B and 9.1-B: one input per message variant.
func queryMessageCases(t *testing.T) map[string]messageCase {
	t.Helper()
	at := func(line int) m.Position { return m.Position{File: "report.sql", Line: line} }
	aggOver := func(e m.Entity, fn, column string) *m.Input {
		return withQueries(src(e), m.Query{
			Kind: m.QuerySelect, Pos: at(1), Relations: []m.Relation{{Name: e.Name, Alias: "x"}},
			Aggregates: []m.Aggregate{{Func: fn, Pos: at(2), Arg: m.ColumnRef{Column: column}}},
		})
	}
	items := placed("items.sql", new(table("order_items", []string{"item_id"},
		col("item_id", m.TypeInt), col("return_id", m.TypeInt))))
	orders := placed("orders.sql", new(table("orders", []string{"order_id"},
		col("order_id", m.TypeInt))))
	lineItems := placed("lines.sql", new(table("order_lines", []string{"line_id"},
		col("line_id", m.TypeInt), col("order_id", m.TypeInt))))
	lineItems.Refs = []m.Ref{{Fields: []string{"order_id"}, Target: "orders"}}
	discounts := func(kv ...string) m.Entity {
		f := fieldWith("discount", m.TypeDecimal, kv...)
		f.Nullable = true
		return placed("orders.sql", new(table("orders", nil, f)))
	}
	balances := placed("balances.sql", new(table("balances",
		[]string{"account_id", "snapshot_date"}, col("account_id", m.TypeInt),
		col("snapshot_date", m.TypeDate), fieldWith("balance", m.TypeDecimal,
			"measure", "semi_additive", "over", "snapshot_date"))))
	stats := placed("stats.sql", new(table("daily_stats", nil,
		fieldWith("conversion", m.TypeDecimal, "measure", "non_additive"))))
	clicks := placed("clicks.sql", new(table("clicks", nil,
		col("clicked_at", m.TypeTimestamptz), col("loaded_at", m.TypeTimestamptz))))
	joinQuery := func(l, lc, r, rc string) m.Query {
		return m.Query{
			Kind: m.QuerySelect, Pos: at(1),
			Relations: []m.Relation{{Name: l, Alias: l}, {Name: r, Alias: r}},
			Joins: []m.Join{{Right: r, Pos: at(3), On: []m.ColumnPair{
				{
					Left: m.ColumnRef{
						Relation: l,
						Column:   lc,
					},
					Right: m.ColumnRef{Relation: r, Column: rc},
				},
			}}},
		}
	}
	return map[string]messageCase{
		"1.5-D no reference": {
			in: withQueries(
				src(items, orders),
				joinQuery("order_items", "return_id", "orders", "order_id"),
			),
			designs: []string{"1.5-D"},
		},
		"1.5-D modeled join": {
			in: withQueries(
				src(lineItems, orders),
				joinQuery("order_lines", "line_id", "orders", "order_id"),
			),
			designs: []string{"1.5-D"},
		},
		"1.5-D modeled join reversed": {
			in: withQueries(src(lineItems, orders),
				joinQuery("orders", "order_id", "order_lines", "line_id")),
			designs: []string{"1.5-D"},
		},
		"5.3-B zero": {
			in:      aggOver(discounts("null", "zero"), "sum", "discount"),
			designs: []string{"5.3-B"},
		},
		"5.3-B zero avg": {
			in:      aggOver(discounts("null", "zero"), "avg", "discount"),
			designs: []string{"5.3-B"},
		},
		"5.3-B zero min": {
			in:      aggOver(discounts("null", "zero"), "min", "discount"),
			designs: []string{"5.3-B"},
		},
		"5.3-B zero max": {
			in:      aggOver(discounts("null", "zero"), "max", "discount"),
			designs: []string{"5.3-B"},
		},
		"5.3-B count": {
			in:      aggOver(discounts("null", "zero"), "count", "discount"),
			designs: []string{"5.3-B"},
		},
		"5.3-B ambiguous": {
			in:      aggOver(discounts(), "avg", "discount"),
			designs: []string{"5.3-B"},
		},
		"8.3-B balance": {in: aggOver(balances, "sum", "balance"), designs: []string{"8.3-B"}},
		"8.4-B average": {in: aggOver(stats, "avg", "conversion"), designs: []string{"8.4-B"}},
		"9.1-B load time": {
			in: withQueries(src(clicks), m.Query{
				Kind:       m.QuerySelect,
				Pos:        at(1),
				Relations:  []m.Relation{{Name: "clicks", Alias: "c"}},
				Aggregates: []m.Aggregate{{Func: "count", Star: true}},
				GroupBy:    []m.ColumnRef{{Column: "loaded_at"}},
			}),
			designs: []string{"9.1-B"},
		},
	}
}
