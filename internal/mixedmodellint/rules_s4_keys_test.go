package mixedmodellint_test

import (
	"testing"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

func TestKeyComposition(t *testing.T) {
	t.Parallel()
	people := table("people", []string{"first_name", "last_name", "birth_date"},
		col("first_name", m.TypeString), col("last_name", m.TypeString),
		col("birth_date", m.TypeDate))
	orders := table("orders", []string{"order_id"}, col("order_id", m.TypeInt))
	lines := referencing(new(table("order_lines", []string{"order_id", "line_no"},
		col("order_id", m.TypeInt), col("line_no", m.TypeInt))), "order_id", "orders")
	cases := map[string]struct {
		entities []m.Entity
		want     []string
	}{
		"a composite natural key": {
			entities: []m.Entity{people},
			want:     []string{"4.4-A people"},
		},
		"a surrogate key":                {entities: []m.Entity{orders}},
		"a reference and a line ordinal": {entities: []m.Entity{orders, lines}},
		"a junction keyed by its references": {
			entities: []m.Entity{
				orders, table("products", []string{"product_id"},
					col("product_id", m.TypeInt)),
				referencing(new(referencing(new(table("order_products",
					[]string{"order_id", "product_id"}, col("order_id", m.TypeInt),
					col("product_id", m.TypeInt))), "order_id", "orders")),
					"product_id", "products"),
			},
		},
		"a reference and a natural column": {
			entities: []m.Entity{orders, referencing(new(table("order_tags",
				[]string{"order_id", "tag"}, col("order_id", m.TypeInt),
				col("tag", m.TypeString))), "order_id", "orders")},
			want: []string{"4.4-A order_tags"},
		},
		"a stored aggregate is keyed by its dimensions": {
			entities: []m.Entity{func() m.Entity {
				e := table("daily_sales", []string{"product_code", "sale_date"},
					col("product_code", m.TypeString), col("sale_date", m.TypeDate))
				e.Annotations = ann("layer", "analytical")
				return e
			}()},
		},
		"a single natural key on one system's entity": {
			entities: []m.Entity{table("currencies", []string{"code"}, col("code", m.TypeString))},
		},
		"a single natural key on a merged entity": {
			entities: []m.Entity{table("customers", []string{"email"}, col("email", m.TypeString),
				col("crm_id", m.TypeString), col("erp_id", m.TypeString))},
			want: []string{"4.4-A customers"},
		},
		"a generated default makes a column surrogate": {
			entities: []m.Entity{table("tokens", []string{"token", "tenant"},
				m.Field{Name: "token", Type: m.TypeString, Default: "gen_random_uuid()"},
				col("tenant", m.TypeString))},
			want: []string{"4.4-A tokens"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.want, findingsOf(src(tc.entities...), "4.4-A"))
		})
	}
}

func TestNaturalIDsIndexed(t *testing.T) {
	t.Parallel()
	customers := func(f m.Field, indexes ...[]string) m.Entity {
		e := table("customers", []string{"customer_id"}, col("customer_id", m.TypeInt), f)
		e.Indexes = indexes
		return e
	}
	cases := map[string]struct {
		entity m.Entity
		want   []string
	}{
		"an unindexed legacy ID": {
			entity: customers(col("legacy_crm_id", m.TypeString)),
			want:   []string{"4.4-B customers legacy_crm_id"},
		},
		"an email led by an index": {
			entity: customers(col("email", m.TypeString), []string{"email"}),
		},
		"an index it is second in does not serve it": {
			entity: customers(col("sku", m.TypeString), []string{"customer_id", "sku"}),
			want:   []string{"4.4-B customers sku"},
		},
		"a unique key leading with it": {
			entity: func() m.Entity {
				e := customers(col("erp_id", m.TypeString))
				e.Uniques = [][]string{{"erp_id"}}
				return e
			}(),
		},
		"a declared natural key": {
			entity: customers(fieldWith("member_no", m.TypeString, "natural_key", "true")),
			want:   []string{"4.4-B customers member_no"},
		},
		"an ordinary attribute": {entity: customers(col("nickname", m.TypeString))},
		"a Go struct has no indexes to check": {
			entity: func() m.Entity {
				e := customers(col("email", m.TypeString))
				e.Source = m.SourceGo
				return e
			}(),
		},
		"a natural primary key is 4.4-A's": {
			entity: table("users", []string{"email"}, col("email", m.TypeString)),
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.want, findingsOf(src(tc.entity), "4.4-B"))
		})
	}
}

func TestDisplayReferences(t *testing.T) {
	t.Parallel()
	customers := table("customers", []string{"customer_id"}, col("customer_id", m.TypeInt),
		col("customer_name", m.TypeString))
	cases := map[string]struct {
		entities []m.Entity
		want     []string
	}{
		"a name of another entity with no ID": {
			entities: []m.Entity{
				customers,
				table("tickets", nil, col("customer_name", m.TypeString)),
			},
			want: []string{"4.5-A tickets customer_name"},
		},
		"the ID beside it": {
			entities: []m.Entity{customers, table("tickets", nil, col("customer_id", m.TypeInt),
				col("customer_name", m.TypeString))},
		},
		"a reference to the entity under another name": {
			entities: []m.Entity{customers, referencing(new(table("tickets", nil,
				col("requester", m.TypeInt), col("customer_email", m.TypeString))),
				"requester", "customers")},
		},
		"an entity's own name": {entities: []m.Entity{customers}},
		"a name with no entity behind it": {
			entities: []m.Entity{table("tickets", nil, col("first_name", m.TypeString))},
		},
		"a role's display value": {
			entities: []m.Entity{table("reviews", nil, col("reviewer_email", m.TypeString))},
			want:     []string{"4.5-A reviews reviewer_email"},
		},
		"a role with its ID": {
			entities: []m.Entity{table("reviews", nil, col("reviewer_id", m.TypeInt),
				col("reviewer_name", m.TypeString))},
		},
		"Go field names": {
			entities: []m.Entity{customers, func() m.Entity {
				e := table("Ticket", nil, col("CustomerName", m.TypeString))
				e.Source = m.SourceGo
				return e
			}()},
			want: []string{"4.5-A Ticket CustomerName"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.want, findingsOf(src(tc.entities...), "4.5-A"))
		})
	}
}

func TestContentDerivedKeys(t *testing.T) {
	t.Parallel()
	images := func(pk string, pkType m.LogicalType, kv ...string) m.Entity {
		return entityOf("images", kv, col(pk, pkType), col("storage_path", m.TypeString),
			col("mime_type", m.TypeString))
	}
	keyed := func(e m.Entity, pk string) m.Entity {
		e.PrimaryKey = []string{pk}
		return e
	}
	cases := map[string]struct {
		entity m.Entity
		want   []string
	}{
		"a file keyed by a hash": {
			entity: keyed(images("sha256", m.TypeString), "sha256"),
			want:   []string{"4.7-A images sha256"},
		},
		"a file keyed by a UUID": {
			entity: keyed(images("image_id", m.TypeUUID), "image_id"),
		},
		"a file keyed by its name": {
			entity: keyed(images("filename", m.TypeString), "filename"),
			want:   []string{"4.7-A images filename"},
		},
		"a file keyed by an integer": {
			entity: keyed(images("image_id", m.TypeInt), "image_id"),
			want:   []string{"4.7-A images image_id"},
		},
		"any entity keyed by a hash": {
			entity: table("documents", []string{"content_md5"}, col("content_md5", m.TypeString)),
			want:   []string{"4.7-A documents content_md5"},
		},
		"a content-addressed store": {
			entity: keyed(images("sha256", m.TypeString, "role", "blob_store"), "sha256"),
		},
		"a word that only contains hash": {
			entity: table("rehashes", []string{"rehashed"}, col("rehashed", m.TypeString)),
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.want, findingsOf(src(tc.entity), "4.7-A"))
		})
	}
}
