package postgres_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	mml "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
	"github.com/StevenACoffman/mixedmodellint/internal/postgres"
)

func equals(t *testing.T, exp, act any) {
	t.Helper()
	if !reflect.DeepEqual(exp, act) {
		t.Fatalf("expected %#v, got %#v", exp, act)
	}
}

func loadFixtures(t *testing.T) mml.Source {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("testdata", "migrations", "*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	var files []postgres.File
	for _, p := range paths {
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, postgres.File{Path: p, Src: src})
	}
	return postgres.Load(files, nil)
}

func entity(t *testing.T, src mml.Source, name string) mml.Entity {
	t.Helper()
	for i := range src.Entities {
		if src.Entities[i].Name == name {
			return src.Entities[i]
		}
	}
	t.Fatalf("no entity %q", name)
	return mml.Entity{}
}

func TestLoadReplaysMigrations(t *testing.T) {
	t.Parallel()
	src := loadFixtures(t)
	orders := entity(t, src, "orders")
	lines := entity(t, src, "order_lines")
	var orderFields []string
	for _, f := range orders.Fields {
		orderFields = append(orderFields, f.Name)
	}
	placed, _ := orders.Field("placed_at")
	amount, _ := lines.Field("amount")
	grain, _ := orders.Annotations.Get("grain")
	layer, _ := orders.Annotations.Get("layer")
	unit, _ := amount.Annotations.Get("unit")

	cases := map[string]struct{ exp, act any }{
		"columns after ADD and DROP": {
			[]string{"order_id", "customer_id", "placed_at", "coupon_code"}, orderFields,
		},
		"ALTER TYPE applied":          {mml.TypeTimestamptz, placed.Type},
		"column primary key":          {[]string{"order_id"}, orders.PrimaryKey},
		"table primary key":           {[]string{"order_id", "line_no"}, lines.PrimaryKey},
		"foreign key":                 {"orders", lines.Refs[0].Target},
		"qualified COMMENT ON finds":  {"one row per order", grain},
		"directive above CREATE":      {"oltp", layer},
		"column comment annotation":   {"USD", unit},
		"raw type keeps modifiers":    {"numeric(12,2)", amount.RawType},
		"primary key is not nullable": {false, lines.Fields[0].Nullable},
		"entity position at name":     {3, orders.Pos.Line},
		"parse error becomes problem": {1, len(src.Problems)},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.exp, tc.act)
		})
	}
}

func TestLoadSnippets(t *testing.T) {
	t.Parallel()
	at := mml.Position{File: "store/store.go", Line: 35, Col: 30}
	src := postgres.Load(nil, []postgres.Snippet{
		{
			SQL:    "SELECT o.id FROM orders o JOIN order_lines l ON l.order_id = o.order_id",
			Pos:    at,
			Strict: true,
		},
		{SQL: "not sql at all", Pos: at, Strict: false},
		{SQL: "SELEC broken", Pos: at, Strict: true},
	})
	cases := map[string]struct{ exp, act any }{
		"one query from the valid snippet": {1, len(src.Queries)},
		"query at the call site":           {at, src.Queries[0].Pos},
		"join at the call site":            {at, src.Queries[0].Joins[0].Pos},
		"strict parse failure reported":    {1, len(src.Problems)},
		"problem at the call site":         {at, src.Problems[0].Pos},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.exp, tc.act)
		})
	}
}
