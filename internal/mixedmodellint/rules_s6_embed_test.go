package mixedmodellint_test

import (
	"slices"
	"testing"

	"pgregory.net/rapid"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// jsonDoc is a JSON Schema entity: the root of file when def is "", or the
// $defs entry def.
func jsonDoc(file, name, def string, fields ...m.Field) m.Entity {
	id := "json:" + file + "#"
	if def != "" {
		id += "/$defs/" + def
	}
	return m.Entity{
		ID: id, Kind: m.KindSchema, Source: m.SourceJSONSchema, Name: name,
		Fields: fields, Annotations: m.Annotations{},
	}
}

// holding returns e with a by-value reference from field to target.
func holding(e *m.Entity, field, target string) m.Entity {
	out := *e
	out.Fields = append(slices.Clone(e.Fields), col(field, m.TypeObject))
	out.Refs = append(slices.Clone(e.Refs),
		m.Ref{Fields: []string{field}, Target: target, ByValue: true})
	return out
}

func TestSharedShapeEmbedding(t *testing.T) {
	t.Parallel()
	person := []m.Field{
		col("name", m.TypeString), col("email", m.TypeString), col("phone", m.TypeString),
		col("tier", m.TypeString),
	}
	customers := table("customers", []string{"customer_id"},
		append([]m.Field{col("customer_id", m.TypeInt)}, person...)...)
	customerDef := jsonDoc("invoice.schema.json", "Customer", "Customer", person...)
	invoice := holding(new(jsonDoc("invoice.schema.json", "Invoice", "",
		col("total", m.TypeDecimal))), "customer", "Customer")
	goCustomer := m.Entity{
		ID: "go:docs.Customer", Kind: m.KindStruct, Source: m.SourceGo, Name: "Customer",
		PrimaryKey: []string{"_id"}, Fields: []m.Field{col("_id", m.TypeString)},
	}
	goInvoice := holding(&m.Entity{
		ID: "go:docs.Invoice", Kind: m.KindStruct, Source: m.SourceGo, Name: "Invoice",
		PrimaryKey: []string{"_id"}, Fields: []m.Field{col("_id", m.TypeString)},
	}, "customer", "Customer")
	line := []m.Field{
		col("sku", m.TypeString), col("qty", m.TypeInt), col("unit_price", m.TypeDecimal),
		col("note", m.TypeString),
	}
	orders := table("orders", []string{"order_id"}, col("order_id", m.TypeInt))
	orderLines := table("order_lines", []string{"order_id", "line_no"},
		append([]m.Field{col("order_id", m.TypeInt), col("line_no", m.TypeInt)}, line...)...)
	orderLines = referencing(&orderLines, "order_id", "orders")
	orderDoc := holding(new(jsonDoc("order.schema.json", "Order", "",
		col("placed_at", m.TypeTimestamptz))), "lines", "Line")
	lineDef := jsonDoc("order.schema.json", "Line", "Line", line...)
	valueDef := customerDef
	valueDef.Annotations = ann("value-object", "true")
	plainRef := invoice
	plainRef.Refs = []m.Ref{{Fields: []string{"customer"}, Target: "Customer"}}
	thin := jsonDoc("invoice.schema.json", "Customer", "Customer", person[:2]...)

	cases := map[string]struct {
		entities []m.Entity
		want     []string
	}{
		"a $defs shape matching a table": {
			entities: []m.Entity{customers, customerDef, invoice},
			want:     []string{"6.5-A Invoice customer"},
		},
		"a keyed document held by value": {
			entities: []m.Entity{goCustomer, goInvoice},
			want:     []string{"6.5-A Invoice customer"},
		},
		"a shape matching nothing stored": {entities: []m.Entity{customerDef, invoice}},
		"a value object":                  {entities: []m.Entity{customers, valueDef, invoice}},
		"lines the parent owns": {
			entities: []m.Entity{orders, orderLines, lineDef, orderDoc},
		},
		"a reference by key":    {entities: []m.Entity{customers, customerDef, plainRef}},
		"too few fields to say": {entities: []m.Entity{customers, thin, invoice}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.want, findingsOf(src(tc.entities...), "6.5-A"))
		})
	}
}

// TestEmbeddingIgnoresParentName checks that what a document is called never
// decides whether its copy of a record is found.
func TestEmbeddingIgnoresParentName(t *testing.T) {
	t.Parallel()
	person := []m.Field{
		col("email", m.TypeString), col("phone", m.TypeString), col("tier", m.TypeString),
	}
	customers := table("customers", []string{"customer_id"},
		append([]m.Field{col("customer_id", m.TypeInt)}, person...)...)
	rapid.Check(t, func(t *rapid.T) {
		name := rapid.StringMatching(`[a-z]{3,10}`).
			Filter(func(s string) bool { return m.Canonical(s) != "customer" }).Draw(t, "name")
		def := jsonDoc(name+".schema.json", "Customer", "Customer", person...)
		parent := holding(new(jsonDoc(name+".schema.json", name, "")), "customer", "Customer")
		got := findingsOf(src(customers, def, parent), "6.5-A")
		if len(got) != 1 {
			t.Fatalf("document %q: expected one finding, got %v", name, got)
		}
	})
}
