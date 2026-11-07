package mixedmodellint_test

import (
	"testing"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// linkedMessageCases covers the cross-source and graph designs 1.6-B, 1.7-A,
// 4.1-B, 5.3-C, 6.2-C and 9.1-C: one input per message variant.
func linkedMessageCases(t *testing.T) map[string]messageCase {
	t.Helper()
	orders := func(fields ...m.Field) m.Entity {
		return placed("orders.sql", new(table("orders", []string{"order_id"},
			append([]m.Field{col("order_id", m.TypeInt)}, fields...)...)))
	}
	goModel := func(fields ...m.Field) m.Entity { return placed("model.go", new(goOrder(fields...))) }
	jsonModel := func(fields ...m.Field) m.Entity {
		return placed("order.schema.json", new(jsonOrder(fields...)))
	}
	lookup := func(name, parent string) m.Entity {
		e := placed(name+".sql", new(table(name, []string{name + "_id"},
			col(name+"_id", m.TypeInt), col("name", m.TypeString), col(parent+"_id", m.TypeInt))))
		if parent != "" {
			e.Refs = []m.Ref{{Fields: []string{parent + "_id"}, Target: parent}}
		}
		return e
	}
	refOrders := orders(col("customer_id", m.TypeInt))
	refOrders.Refs = []m.Ref{{
		Fields: []string{"customer_id"}, Target: "customers",
		Pos: m.Position{File: "orders.sql", Line: 9},
	}}
	goRef := goModel(col("OrderID", m.TypeInt), col("CustomerID", m.TypeInt))
	goRef.Refs = []m.Ref{{
		Fields: []string{"CustomerID"}, Target: "Customer",
		Pos: m.Position{File: "model.go", Line: 7},
	}}
	return map[string]messageCase{
		"1.6-B chain": {
			in: src(lookup("address", "city"), lookup("city", "state"),
				lookup("state", "country"), lookup("country", "")),
			designs: []string{"1.6-B"},
		},
		"1.7-A query": {
			in: withQueries(src(placed("tmp.sql", new(table("tmp_orders", nil,
				col("order_id", m.TypeInt))))), m.Query{
				Kind: m.QuerySelect, Pos: m.Position{File: "report.sql", Line: 3},
				Relations: []m.Relation{{Name: "tmp_orders", Alias: "tmp_orders"}},
			}),
			designs: []string{"1.7-A"},
		},
		"4.1-B missing key": {
			in: src(orders(), jsonModel(col("total", m.TypeDecimal))), designs: []string{"4.1-B"},
		},
		"4.1-B optional key": {
			in:      src(orders(), jsonModel(nullableCol("order_id", m.TypeInt))),
			designs: []string{"4.1-B"},
		},
		"4.1-B key type": {
			in: src(orders(), goModel(col("OrderID", m.TypeString))), designs: []string{"4.1-B"},
		},
		"5.3-C Go": {
			in: src(orders(nullableCol("note", m.TypeString)),
				goModel(col("OrderID", m.TypeInt), col("Note", m.TypeString))),
			designs: []string{"5.3-C"},
		},
		"5.3-C SQL": {
			in: src(orders(col("note", m.TypeString)),
				jsonModel(col("order_id", m.TypeInt), nullableCol("note", m.TypeString))),
			designs: []string{"5.3-C"},
		},
		"5.3-C JSON": {
			in: src(orders(nullableCol("note", m.TypeString)),
				jsonModel(col("order_id", m.TypeInt), col("note", m.TypeString))),
			designs: []string{"5.3-C"},
		},
		"6.2-C not declared": {
			in: src(orders(col("customer_id", m.TypeInt)), goRef,
				table("customers", []string{"customer_id"}, col("customer_id", m.TypeInt))),
			designs: []string{"6.2-C"},
		},
		"6.2-C optional": {
			in: src(refOrders, jsonModel(col("order_id", m.TypeInt),
				nullableCol("customer_id", m.TypeInt))),
			designs: []string{"6.2-C"},
		},
		"1.7-A view": {
			in: withQueries(src(placed("tmp.sql", new(table("tmp_orders", nil,
				col("order_id", m.TypeInt))))), m.Query{
				Kind: m.QueryCreateView, Target: "order_report",
				Pos:       m.Position{File: "views.sql", Line: 3},
				Relations: []m.Relation{{Name: "tmp_orders", Alias: "tmp_orders"}},
			}),
			designs: []string{"1.7-A"},
		},
		"1.7-A table": {
			in: src(placed("tmp.sql", new(table("tmp_orders", []string{"order_id"},
				col("order_id", m.TypeInt)))), func() m.Entity {
				e := placed("lines.sql", new(table("order_lines", nil, col("order_id", m.TypeInt))))
				e.Refs = []m.Ref{{Fields: []string{"order_id"}, Target: "tmp_orders"}}
				return e
			}()),
			designs: []string{"1.7-A"},
		},
		"9.1-C nothing stored": {
			in: src(orders(), jsonModel(col("order_id", m.TypeInt),
				col("placed_at", m.TypeTimestamptz))),
			designs: []string{"9.1-C"},
		},
		"9.1-C dropped": {
			in: src(orders(col("loaded_at", m.TypeTimestamptz)),
				jsonModel(col("order_id", m.TypeInt), col("placed_at", m.TypeTimestamptz))),
			designs: []string{"9.1-C"},
		},
	}
}
