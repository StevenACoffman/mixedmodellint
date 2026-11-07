package mixedmodellint_test

import (
	"testing"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

func TestJoinKeyConformance(t *testing.T) {
	t.Parallel()
	orders := table("orders", []string{"order_id"}, col("order_id", m.TypeInt),
		col("customer_id", m.TypeInt), col("store_id", m.TypeInt), col("note", m.TypeString))
	orders.Refs = []m.Ref{
		{Fields: []string{"customer_id"}, Target: "customers"},
		{Fields: []string{"store_id"}, Target: "stores"},
	}
	items := table("order_items", []string{"item_id"}, col("item_id", m.TypeInt),
		col("order_id", m.TypeInt), col("return_id", m.TypeInt))
	items.Refs = []m.Ref{
		{Fields: []string{"order_id"}, Target: "orders"},
		{Fields: []string{"return_id"}, Target: "returns", Inferred: true},
	}
	visits := table("visits", []string{"visit_id"}, col("visit_id", m.TypeInt),
		col("store_id", m.TypeInt), col("shop_id", m.TypeInt), col("note", m.TypeString))
	visits.Refs = []m.Ref{
		{Fields: []string{"store_id"}, Target: "stores"},
		{Fields: []string{"shop_id"}, Target: "stores"},
	}
	tickets := table("tickets", []string{"ticket_id"}, col("ticket_id", m.TypeInt),
		col("store_code", m.TypeString))
	tickets.Refs = []m.Ref{{
		Fields: []string{"store_code"}, Target: "stores",
		TargetFields: []string{"code"},
	}}
	matches := entityOf("customer_matches", []string{"role", "identity_map"},
		col("email", m.TypeString))
	customers := table("customers", []string{"customer_id"}, col("customer_id", m.TypeInt),
		col("email", m.TypeString))
	stores := table("stores", []string{"store_id"}, col("store_id", m.TypeInt),
		col("code", m.TypeString))
	join := func(l, lc, r, rc string) m.Query {
		return m.Query{
			Kind: m.QuerySelect, Pos: m.Position{File: "report.sql", Line: 1},
			Relations: []m.Relation{{Name: l, Alias: l}, {Name: r, Alias: r}},
			Joins: []m.Join{{
				Right: r, Pos: m.Position{File: "report.sql", Line: 2},
				On: []m.ColumnPair{{
					Left:  m.ColumnRef{Relation: l, Column: lc},
					Right: m.ColumnRef{Relation: r, Column: rc},
				}},
			}},
		}
	}
	cases := map[string]struct {
		q    m.Query
		want []string
	}{
		"along a declared reference": {q: join("order_items", "order_id", "orders", "order_id")},
		"along a reference, written the other way": {
			q: join("orders", "order_id", "order_items", "order_id"),
		},
		"the wrong key": {
			q:    join("order_items", "return_id", "orders", "order_id"),
			want: []string{"1.5-D order_items return_id"},
		},
		"a reference to another entity": {
			q:    join("orders", "customer_id", "order_items", "order_id"),
			want: []string{"1.5-D orders customer_id"},
		},
		"through a shared dimension": {q: join("orders", "store_id", "visits", "store_id")},
		"an identity map matches on attributes": {
			q: join("customer_matches", "email", "customers", "email"),
		},
		"attributes of two entities": {
			q:    join("orders", "store_id", "customers", "customer_id"),
			want: []string{"1.5-D orders store_id"},
		},
		"a self-join is not checked": {q: join("orders", "customer_id", "orders", "order_id")},
		"a column the catalog does not know": {
			q: join("orders", "region", "customers", "customer_id"),
		},
		"a right-hand column the catalog does not know": {
			q: join("orders", "customer_id", "customers", "region"),
		},
		"an identity map on the right": {
			q: join("customers", "email", "customer_matches", "email"),
		},
		"a sanctioned pair, then a wrong one": {
			q: func() m.Query {
				q := join("order_items", "order_id", "orders", "order_id")
				q.Joins[0].On = append(q.Joins[0].On, m.ColumnPair{
					Left:  m.ColumnRef{Relation: "order_items", Column: "return_id"},
					Right: m.ColumnRef{Relation: "orders", Column: "order_id"},
				})
				return q
			}(),
			want: []string{"1.5-D order_items return_id"},
		},
		"a dimension reached under different names": {
			q:    join("orders", "store_id", "visits", "shop_id"),
			want: []string{"1.5-D orders store_id"},
		},
		"same-named columns that reference nothing": {
			q:    join("orders", "note", "visits", "note"),
			want: []string{"1.5-D orders note"},
		},
		"a reference to declared target columns": {
			q: join("tickets", "store_code", "stores", "code"),
		},
		"the target's key when the reference names other columns": {
			q:    join("tickets", "store_code", "stores", "store_id"),
			want: []string{"1.5-D tickets store_code"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.want, findingsOf(&m.Input{Sources: []m.Source{{
				Entities: []m.Entity{orders, items, visits, matches, customers, stores, tickets},
				Queries:  []m.Query{tc.q},
			}}}, "1.5-D"))
		})
	}
}
