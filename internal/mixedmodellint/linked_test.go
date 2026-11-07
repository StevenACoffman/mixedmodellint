package mixedmodellint_test

import (
	"slices"
	"testing"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// goOrder and jsonOrder are the Go and JSON Schema members of the logical
// entity order, with the fields given, for the cross-source tables.
func goOrder(fields ...m.Field) m.Entity {
	return m.Entity{
		ID: "go:model.Order", Kind: m.KindStruct, Source: m.SourceGo, Name: "Order",
		Fields: fields, Annotations: m.Annotations{}, Pos: m.Position{File: "model.go", Line: 3},
	}
}

func jsonOrder(fields ...m.Field) m.Entity {
	return m.Entity{
		ID: "json:order.schema.json", Kind: m.KindSchema, Source: m.SourceJSONSchema,
		Name: "order", Fields: fields, Annotations: m.Annotations{},
		Pos: m.Position{File: "order.schema.json", Line: 1},
	}
}

func nullableCol(name string, t m.LogicalType) m.Field {
	return m.Field{Name: name, Type: t, Nullable: true}
}

func TestCrossFormNullability(t *testing.T) {
	t.Parallel()
	customerRef := m.Ref{Fields: []string{"customer_id"}, Target: "customers"}
	cases := map[string]struct {
		sql  m.Entity
		peer m.Entity
		want []string
	}{
		"both required": {
			sql: table(
				"orders",
				[]string{"order_id"},
				col("order_id", m.TypeInt),
				col("note", m.TypeString),
			),
			peer: goOrder(col("OrderID", m.TypeInt), col("Note", m.TypeString)),
		},
		"nullable in SQL, a plain string in Go": {
			sql: table("orders", []string{"order_id"}, col("order_id", m.TypeInt),
				nullableCol("note", m.TypeString)),
			peer: goOrder(col("OrderID", m.TypeInt), col("Note", m.TypeString)),
			want: []string{"5.3-C Order Note"},
		},
		"optional in JSON, NOT NULL in SQL": {
			sql: table("orders", []string{"order_id"}, col("order_id", m.TypeInt),
				col("note", m.TypeString)),
			peer: jsonOrder(col("order_id", m.TypeInt), nullableCol("note", m.TypeString)),
			want: []string{"5.3-C orders note"},
		},
		"a field only one form has": {
			sql:  table("orders", []string{"order_id"}, col("order_id", m.TypeInt)),
			peer: goOrder(col("OrderID", m.TypeInt), nullableCol("Note", m.TypeString)),
		},
		"a reference field is left to 6.2-C": {
			sql: func() m.Entity {
				e := table("orders", []string{"order_id"}, col("order_id", m.TypeInt),
					nullableCol("customer_id", m.TypeInt))
				e.Refs = []m.Ref{customerRef}
				return e
			}(),
			peer: goOrder(col("OrderID", m.TypeInt), col("CustomerID", m.TypeInt)),
		},
		"an inferred reference is still compared here": {
			sql: func() m.Entity {
				e := table("orders", []string{"order_id"}, col("order_id", m.TypeInt),
					nullableCol("customer_id", m.TypeInt))
				e.Refs = []m.Ref{{
					Fields: []string{"customer_id"}, Target: "customers",
					Inferred: true,
				}}
				return e
			}(),
			peer: goOrder(col("OrderID", m.TypeInt), col("CustomerID", m.TypeInt)),
			want: []string{"5.3-C Order CustomerID"},
		},
		"entities that do not link": {
			sql:  table("payments", nil, nullableCol("note", m.TypeString)),
			peer: goOrder(col("Note", m.TypeString)),
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// A second SQL form of the same entity, listed between them, is
			// not compared with the first but is with the peer.
			archive := tc.sql
			archive.ID = "sql:archive." + tc.sql.Name
			archive.Fields = slices.Clone(tc.sql.Fields)
			for i := range archive.Fields {
				archive.Fields[i].Nullable = !archive.Fields[i].Nullable
			}
			got := findingsOf(&m.Input{Sources: []m.Source{{
				Entities: []m.Entity{tc.sql, archive, tc.peer},
			}}}, "5.3-C")
			equals(t, tc.want, findingsOf(&m.Input{Sources: []m.Source{{
				Entities: []m.Entity{tc.sql, tc.peer},
			}}}, "5.3-C"))
			if len(tc.want) > 0 && len(got) <= len(tc.want) {
				t.Errorf("with a second SQL form, got %v, want more than %v", got, tc.want)
			}
		})
	}
}

func TestCrossSourceReferenceParity(t *testing.T) {
	t.Parallel()
	withRef := func(e m.Entity, inferred bool) m.Entity {
		e.Refs = []m.Ref{{
			Fields: []string{e.Fields[1].Name}, Target: "customers",
			Inferred: inferred,
		}}
		return e
	}
	sqlOrders := func(customer m.Field) m.Entity {
		return table("orders", []string{"order_id"}, col("order_id", m.TypeInt), customer)
	}
	cases := map[string]struct {
		sql  m.Entity
		peer m.Entity
		want []string
	}{
		"declared and required in both": {
			sql:  withRef(sqlOrders(col("customer_id", m.TypeInt)), false),
			peer: withRef(goOrder(col("OrderID", m.TypeInt), col("CustomerID", m.TypeInt)), false),
		},
		"declared in Go, missing in SQL": {
			sql:  sqlOrders(col("customer_id", m.TypeInt)),
			peer: withRef(goOrder(col("OrderID", m.TypeInt), col("CustomerID", m.TypeInt)), false),
			want: []string{"6.2-C orders customer_id"},
		},
		"only inferred in SQL": {
			sql:  withRef(sqlOrders(col("customer_id", m.TypeInt)), true),
			peer: withRef(goOrder(col("OrderID", m.TypeInt), col("CustomerID", m.TypeInt)), false),
			want: []string{"6.2-C orders customer_id"},
		},
		"an inferred Go reference is no evidence": {
			sql:  sqlOrders(col("customer_id", m.TypeInt)),
			peer: withRef(goOrder(col("OrderID", m.TypeInt), col("CustomerID", m.TypeInt)), true),
		},
		"required in SQL, optional in JSON": {
			sql:  withRef(sqlOrders(col("customer_id", m.TypeInt)), false),
			peer: jsonOrder(col("order_id", m.TypeInt), nullableCol("customer_id", m.TypeInt)),
			want: []string{"6.2-C order customer_id"},
		},
		"a JSON ID need not declare the reference": {
			sql:  withRef(sqlOrders(col("customer_id", m.TypeInt)), false),
			peer: jsonOrder(col("order_id", m.TypeInt), col("customer_id", m.TypeInt)),
		},
		"a foreign key on another column only": {
			sql: func() m.Entity {
				e := sqlOrders(col("customer_id", m.TypeInt))
				e.Fields = append(e.Fields, col("store_id", m.TypeInt))
				e.Refs = []m.Ref{{Fields: []string{"store_id"}, Target: "stores"}}
				return e
			}(),
			peer: withRef(goOrder(col("OrderID", m.TypeInt), col("CustomerID", m.TypeInt)), false),
			want: []string{"6.2-C orders customer_id"},
		},
		"an inferred reference, then a declared one": {
			sql: sqlOrders(col("customer_id", m.TypeInt)),
			peer: func() m.Entity {
				g := goOrder(col("OrderID", m.TypeInt), col("CustomerID", m.TypeInt),
					col("StoreID", m.TypeInt))
				g.Refs = []m.Ref{
					{Fields: []string{"StoreID"}, Target: "stores", Inferred: true},
					{Fields: []string{"Ghost"}, Target: "ghosts"},
					{Fields: []string{"CustomerID"}, Target: "customers"},
				}
				return g
			}(),
			want: []string{"6.2-C orders customer_id"},
		},
		"optional in the declaring form": {
			sql:  withRef(sqlOrders(nullableCol("customer_id", m.TypeInt)), false),
			peer: jsonOrder(col("order_id", m.TypeInt), nullableCol("customer_id", m.TypeInt)),
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			customers := table("customers", []string{"customer_id"}, col("customer_id", m.TypeInt))
			// A JSON form without the field comes first, and must not stop
			// the comparison with the forms after it.
			bare := jsonOrder(col("order_id", m.TypeInt))
			equals(t, tc.want, findingsOf(&m.Input{Sources: []m.Source{{
				Entities: []m.Entity{customers, bare, tc.sql, tc.peer},
			}}}, "6.2-C"))
		})
	}
}

func TestIdentifierContinuity(t *testing.T) {
	t.Parallel()
	sqlOrders := table("orders", []string{"order_id"}, col("order_id", m.TypeInt))
	digits := fieldWith("order_id", m.TypeString, "pattern", `^\d+$`)
	cases := map[string]struct {
		members []m.Entity
		want    []string
	}{
		"the key in every form": {
			members: []m.Entity{
				sqlOrders, goOrder(col("OrderID", m.TypeInt)),
				jsonOrder(col("order_id", m.TypeInt)),
			},
		},
		"no key in the JSON form": {
			members: []m.Entity{sqlOrders, jsonOrder(col("total", m.TypeDecimal))},
			want:    []string{"4.1-B order"},
		},
		"an optional key": {
			members: []m.Entity{sqlOrders, jsonOrder(nullableCol("order_id", m.TypeInt))},
			want:    []string{"4.1-B order order_id"},
		},
		"a string key for an integer": {
			members: []m.Entity{sqlOrders, jsonOrder(col("order_id", m.TypeString))},
			want:    []string{"4.1-B order order_id"},
		},
		"a digits-only string": {members: []m.Entity{sqlOrders, jsonOrder(digits)}},
		"a UUID held as a string": {
			members: []m.Entity{
				table("orders", []string{"order_id"}, col("order_id", m.TypeUUID)),
				goOrder(col("OrderID", m.TypeString)),
			},
		},
		"a Go key when there is no SQL form": {
			members: []m.Entity{
				func() m.Entity {
					g := goOrder(col("OrderID", m.TypeInt))
					g.PrimaryKey = []string{"OrderID"}
					return g
				}(),
				jsonOrder(col("total", m.TypeDecimal)),
			},
			want: []string{"4.1-B order"},
		},
		"the SQL key wins over a Go key listed first": {
			members: []m.Entity{
				func() m.Entity {
					g := goOrder(col("OrderID", m.TypeString))
					g.PrimaryKey = []string{"OrderID"}
					return g
				}(),
				sqlOrders, jsonOrder(col("order_id", m.TypeInt)),
			},
			want: []string{"4.1-B Order OrderID"},
		},
		"a key column that is not a field is skipped": {
			members: []m.Entity{
				func() m.Entity {
					e := table("orders", []string{"ghost", "order_id"}, col("order_id", m.TypeInt))
					return e
				}(),
				jsonOrder(col("order_id", m.TypeString)),
			},
			want: []string{"4.1-B order order_id"},
		},
		"a string key for a UUID, either way round": {
			members: []m.Entity{
				table("orders", []string{"order_id"}, col("order_id", m.TypeString)),
				goOrder(col("OrderID", m.TypeUUID)),
			},
		},
		"a bracketed digits pattern": {
			members: []m.Entity{
				sqlOrders,
				jsonOrder(fieldWith("order_id", m.TypeString, "pattern", "^[0-9]+$")),
			},
		},
		"a string with another pattern": {
			members: []m.Entity{
				sqlOrders,
				jsonOrder(fieldWith("order_id", m.TypeString, "pattern", "^[a-z]+$")),
			},
			want: []string{"4.1-B order order_id"},
		},
		"an integer key held as a decimal": {
			members: []m.Entity{sqlOrders, jsonOrder(col("order_id", m.TypeDecimal))},
			want:    []string{"4.1-B order order_id"},
		},
		"two SQL forms are not compared with each other": {
			members: []m.Entity{sqlOrders, func() m.Entity {
				e := table("orders", nil, col("total", m.TypeDecimal))
				e.ID = "sql:archive.orders"
				return e
			}()},
		},
		"no member has a key": {
			members: []m.Entity{
				goOrder(col("Total", m.TypeDecimal)),
				jsonOrder(col("total", m.TypeDecimal)),
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// A group with no key comes first, so skipping it must not stop
			// the check.
			unkeyed := []m.Entity{
				table("carts", nil, col("total", m.TypeDecimal)),
				{
					ID: "go:model.Cart", Kind: m.KindStruct, Source: m.SourceGo, Name: "Cart",
					Fields: []m.Field{col("Total", m.TypeDecimal)}, Annotations: m.Annotations{},
				},
			}
			equals(t, tc.want, findingsOf(&m.Input{Sources: []m.Source{{
				Entities: append(unkeyed, tc.members...),
			}}}, "4.1-B"))
		})
	}
}

func TestCrossSourceTimeRoles(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		sql  m.Entity
		peer m.Entity
		want []string
	}{
		"event time kept": {
			sql:  table("orders", nil, col("placed_at", m.TypeTimestamptz)),
			peer: jsonOrder(col("placed_at", m.TypeTimestamptz)),
		},
		"event time dropped for load time": {
			sql:  table("orders", nil, col("loaded_at", m.TypeTimestamptz)),
			peer: jsonOrder(col("placed_at", m.TypeTimestamptz)),
			want: []string{"9.1-C orders"},
		},
		"no time stored at all": {
			sql:  table("orders", nil, col("order_id", m.TypeInt)),
			peer: goOrder(col("PlacedAt", m.TypeTimestamptz)),
			want: []string{"9.1-C orders"},
		},
		"a store with its own event time under another name": {
			sql:  table("orders", nil, col("ordered_at", m.TypeTimestamptz)),
			peer: jsonOrder(col("placed_at", m.TypeTimestamptz)),
		},
		"no event time upstream": {
			sql:  table("orders", nil, col("loaded_at", m.TypeTimestamptz)),
			peer: jsonOrder(col("synced_at", m.TypeTimestamptz)),
		},
		"an event-named string is not event time": {
			sql:  table("orders", nil, col("loaded_at", m.TypeTimestamptz)),
			peer: jsonOrder(col("order_id", m.TypeInt), col("placed_at", m.TypeString)),
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.want, findingsOf(&m.Input{Sources: []m.Source{{
				Entities: []m.Entity{tc.sql, tc.peer},
			}}}, "9.1-C"))
		})
	}
}

// TestCrossSourceTimeRolesPerStore checks 9.1-C over a group with several
// members: each SQL store is judged on its own, and one finding names the
// first upstream form with event time.
func TestCrossSourceTimeRolesPerStore(t *testing.T) {
	t.Parallel()
	keeps := table("orders", nil, col("placed_at", m.TypeTimestamptz))
	drops := table("orders", nil, col("loaded_at", m.TypeTimestamptz))
	drops.ID = "sql:archive.orders"
	cases := map[string]struct {
		members []m.Entity
		want    []string
	}{
		"a store that keeps it, then one that drops it": {
			members: []m.Entity{keeps, drops, jsonOrder(col("placed_at", m.TypeTimestamptz))},
			want:    []string{"9.1-C orders"},
		},
		"two upstream forms, one finding": {
			members: []m.Entity{
				drops, goOrder(col("PlacedAt", m.TypeTimestamptz)),
				jsonOrder(col("placed_at", m.TypeTimestamptz)),
			},
			want: []string{"9.1-C orders"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.want, findingsOf(&m.Input{Sources: []m.Source{{
				Entities: tc.members,
			}}}, "9.1-C"))
		})
	}
}
