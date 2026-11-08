package mixedmodellint_test

import (
	"fmt"
	"testing"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// wide returns a table with n columns.
func wide(name string, n int, kv ...string) m.Entity {
	fields := make([]m.Field, n)
	for i := range fields {
		fields[i] = col(fmt.Sprintf("attr_%d", i), m.TypeString)
	}
	return entityOf(name, kv, fields...)
}

func TestOBTLayerContract(t *testing.T) {
	t.Parallel()
	core := entityOf("orders", []string{"layer", "core"}, col("order_id", m.TypeInt))
	staging := entityOf("stg_orders", []string{"layer", "staging"}, col("order_id", m.TypeInt))
	contract := []string{"layer", "obt", "grain", "one row per order", "upstream", "orders"}
	cases := map[string]struct {
		entities []m.Entity
		want     []string
	}{
		"a wide table with no contract": {
			entities: []m.Entity{wide("order_report", 50)},
			want:     []string{"3.3-B order_report"},
		},
		"a narrower table is not an OBT": {
			entities: []m.Entity{wide("order_report", 49)},
		},
		"an obt_ name makes a narrow table one": {
			entities: []m.Entity{wide("obt_orders", 3)},
			want:     []string{"3.3-B obt_orders"},
		},
		"layer=obt makes a narrow table one": {
			entities: []m.Entity{wide("order_report", 3, "layer", "obt"), core},
			want:     []string{"3.3-B order_report"},
		},
		"a full contract on core upstream": {
			entities: []m.Entity{wide("order_report", 60, contract...), core},
		},
		"the upstream is a list in brackets": {
			entities: []m.Entity{
				wide("order_report", 60, "layer", "obt", "grain", "one row per order",
					"upstream", "[orders, customers]"), core,
				entityOf("customers", []string{"layer", "core"}, col("customer_id", m.TypeInt)),
			},
		},
		"the upstream names an unknown entity": {
			entities: []m.Entity{wide("order_report", 60, contract...)},
			want:     []string{"3.3-B order_report"},
		},
		"the upstream is not core": {
			entities: []m.Entity{
				wide("order_report", 60, "layer", "obt", "grain", "one row per order",
					"upstream", "stg_orders"),
				staging,
			},
			want: []string{"3.3-B order_report"},
		},
		"a vendor's wide table is not ours to model": {
			entities: []m.Entity{wide("erp_items", 80, "source", "legacy")},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.want, findingsOf(src(tc.entities...), "3.3-B"))
		})
	}
}

func TestForeignSchemaMutation(t *testing.T) {
	t.Parallel()
	vendor := func(kv []string, changes ...m.Change) m.Entity {
		e := entityOf("vendor_orders", kv, col("id", m.TypeInt))
		e.Pos = m.Position{File: "db/001_vendor.sql", Line: 1}
		e.Changes = changes
		return e
	}
	later := func(stmt string) m.Change {
		return m.Change{Statement: stmt, Pos: m.Position{File: "db/007_extend.sql", Line: 3}}
	}
	snapshot := m.Change{
		Statement: "CREATE INDEX",
		Pos:       m.Position{File: "db/001_vendor.sql", Line: 9},
	}
	cases := map[string]struct {
		entity m.Entity
		want   []string
	}{
		"a later ALTER on a third-party table": {
			entity: vendor([]string{"source", "third_party"}, later("ALTER TABLE")),
			want:   []string{"3.5-A vendor_orders"},
		},
		"each change is reported where it is": {
			entity: vendor(
				[]string{"source", "legacy"},
				later("CREATE INDEX"),
				later("CREATE TRIGGER"),
			),
			want: []string{"3.5-A vendor_orders", "3.5-A vendor_orders"},
		},
		"the vendor's own indexes, in the copied schema": {
			entity: vendor([]string{"source", "third_party"}, snapshot),
		},
		"a table the team owns may change": {
			entity: vendor(nil, later("ALTER TABLE")),
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.want, findingsOf(src(tc.entity), "3.5-A"))
		})
	}
}
