package gopackages_test

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/StevenACoffman/mixedmodellint/internal/gopackages"
	mml "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

func equals(t *testing.T, exp, act any) {
	t.Helper()
	if !reflect.DeepEqual(exp, act) {
		t.Fatalf("expected %#v, got %#v", exp, act)
	}
}

func load(t *testing.T) gopackages.Result {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("testdata", "mod"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := gopackages.Load(t.Context(), dir, dir)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestLoadEntities(t *testing.T) {
	t.Parallel()
	res := load(t)
	byName := map[string]mml.Entity{}
	for i := range res.Source.Entities {
		byName[res.Source.Entities[i].Name] = res.Source.Entities[i]
	}
	order, line := byName["Order"], byName["OrderLine"]
	var orderFields []string
	for i := range order.Fields {
		orderFields = append(orderFields, order.Fields[i].Name)
	}
	grain, _ := order.Annotations.Get("grain")
	status, _ := order.Field("status")
	updated, _ := order.Field("updated_at")

	cases := map[string]struct{ exp, act any }{
		"no load problems":    {0, len(res.Source.Problems)},
		"stored structs only": {2, len(res.Source.Entities)},
		"embedded across packages": {
			[]string{"id", "customer_id", "status", "created_at", "updated_at"},
			orderFields,
		},
		"id from package path":            {"go:example.com/mod/model.Order", order.ID},
		"directive read":                  {"one row per order", grain},
		"named string type is a string":   {mml.TypeString, status.Type},
		"pointer is nullable":             {true, updated.Nullable},
		"mml pk tags":                     {[]string{"order_id", "line_no"}, line.PrimaryKey},
		"position relative to the module": {"model/model.go", order.Pos.File},
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
	res := load(t)
	type snip struct {
		SQL    string
		Line   int
		Strict bool
	}
	var got []snip
	for _, s := range res.Snippets {
		got = append(got, snip{s.SQL, s.Pos.Line, s.Strict})
	}
	want := []snip{
		{"SELECT id, status FROM orders", 35, true},
		{"SELECT id FROM orders WHERE status = 'open' ORDER BY id", 45, true},
		{"SELECT id FROM orders WHERE status <> 'open' ORDER BY id", 45, true},
		{"UPDATE orders SET updated_at = now()", 51, false},
	}
	equals(t, want, got)
	equals(t, "store/store.go", res.Snippets[0].Pos.File)
}

func TestLoadWithTypeErrors(t *testing.T) {
	t.Parallel()
	dir, err := filepath.Abs(filepath.Join("testdata", "broken"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := gopackages.Load(t.Context(), dir, dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for i := range res.Source.Entities {
		names = append(names, res.Source.Entities[i].Name)
	}
	equals(t, 1, len(res.Source.Problems))
	p := res.Source.Problems[0]
	equals(t, mml.Position{File: "bad/bad.go", Line: 9, Col: 18}, p.Pos)
	equals(t, []string{"Thing", "Item"}, names)
}
