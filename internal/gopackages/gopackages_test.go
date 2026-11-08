package gopackages_test

import (
	"fmt"
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
	fee, _ := order.Field("shipping_fee")
	created, _ := order.Field("created_at")
	note, _ := line.Field("note")

	cases := map[string]struct{ exp, act any }{
		"no load problems":    {0, len(res.Source.Problems)},
		"stored structs only": {2, len(res.Source.Entities)},
		"embedded across packages": {
			[]string{
				"id", "customer_id", "status", "shipping_fee", "placed_at", "created_at",
				"updated_at",
			},
			orderFields,
		},
		"id from package path":            {"go:example.com/mod/model.Order", order.ID},
		"directive read":                  {"one row per order", grain},
		"named string type is a string":   {mml.TypeString, status.Type},
		"pointer is nullable":             {true, updated.Nullable},
		"mml pk tags":                     {[]string{"order_id", "line_no"}, line.PrimaryKey},
		"position relative to the module": {"model/model.go", order.Pos.File},
		"field doc comment": {
			"ShippingFee is what the customer pays for delivery, in cents.", fee.Doc,
		},
		"field line comment": {"what the customer asked for", note.Doc},
		"embedded field doc comment": {
			"CreatedAt is when the row was first written.",
			created.Doc,
		},
		"undocumented field has none": {"", updated.Doc},
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
		if s.Pos.File == "store/store.go" {
			got = append(got, snip{s.SQL, s.Pos.Line, s.Strict})
		}
	}
	want := []snip{
		{"SELECT id, status FROM orders", 35, true},
		{"SELECT id FROM orders WHERE status = 'open' ORDER BY id", 45, true},
		{"SELECT id FROM orders WHERE status <> 'open' ORDER BY id", 45, true},
		{"UPDATE orders SET updated_at = now()", 51, false},
	}
	// Positions are relative to the module, or no snippet matches the file.
	equals(t, want, got)
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

// TestSnippetArgs pins what the tracer learns about each value bound to a
// parameter, written as "from@line/truncation" per origin.
func TestSnippetArgs(t *testing.T) {
	t.Parallel()
	res := load(t)
	got := map[string][][]string{}
	for _, s := range res.Snippets {
		if s.Pos.File != "store/writes.go" {
			continue
		}
		var args [][]string
		for _, origins := range s.Args {
			var desc []string
			for _, o := range origins {
				desc = append(desc, fmt.Sprintf("%s.%s@%d/%s",
					o.From.Entity, o.From.Field, o.Pos.Line, o.Truncate))
			}
			args = append(args, desc)
		}
		got[s.SQL] = args
	}
	cases := map[string]struct {
		sql  string
		want [][]string
	}{
		"sqlc Params filled from a persisted struct": {
			sql:  "-- name: CreateLine :exec\nINSERT INTO order_lines (order_id, order_fee) VALUES ($1, $2)\n",
			want: [][]string{{"Order.id@32/"}, {"Order.shipping_fee@33/"}},
		},
		"truncation, location change and format": {
			sql: "INSERT INTO clicks (clicked_at, placed_at, placed_on) VALUES ($1, $2, $3)",
			want: [][]string{
				{".@41/second"}, {"Order.placed_at@42/"}, {"Order.placed_at@43/day"},
			},
		},
		"round trips through Unix and UnixMilli": {
			sql:  "UPDATE clicks SET seen_at = $1",
			want: [][]string{{".@50/second", ".@52/millisecond"}},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.want, got[tc.sql])
		})
	}
}

// describe writes origins as "from@line/truncation", the form the tracer
// tests compare.
func describe(origins []mml.Origin) []string {
	var out []string
	for _, o := range origins {
		out = append(
			out,
			fmt.Sprintf("%s.%s@%d/%s", o.From.Entity, o.From.Field, o.Pos.Line, o.Truncate),
		)
	}
	return out
}

// TestORMsAndWrappers pins what the tracer learns from sqlx named
// parameters, a wrapper around ExecContext, and gorm's Create.
func TestORMsAndWrappers(t *testing.T) {
	t.Parallel()
	res := load(t)
	got := map[string][][]string{}
	var lines []int
	for _, s := range res.Snippets {
		if s.Pos.File != "store/orm.go" {
			continue
		}
		lines = append(lines, s.Pos.Line)
		var args [][]string
		for _, origins := range s.Args {
			args = append(args, describe(origins))
		}
		got[s.SQL] = args
	}
	equals(t, []int{21, 38}, lines) // the wrapper's own call is not a snippet; its caller's is
	equals(t, [][]string{{"Order.id@24/"}, {"Order.shipping_fee@25/"}},
		got["INSERT INTO order_lines (order_id, order_fee) VALUES ($1, $2::numeric)"])
	equals(t, [][]string{{"Order.placed_at@38/hour"}}, got["UPDATE clicks SET seen_at = $1"])

	equals(t, 1, len(res.Source.Queries))
	q := res.Source.Queries[0]
	equals(t, "OrderLine", q.Target)
	equals(t, mml.QueryInsert, q.Kind)
	equals(t, 43, q.Pos.Line)
	var cols []string
	for _, a := range q.Assignments {
		cols = append(cols, a.Column)
	}
	equals(t, []string{"order_id", "line_no", "note"}, cols)
	equals(t, []string{"Order.id@44/"}, describe(q.Assignments[0].Origins))
	equals(t, []string(nil), describe(q.Assignments[2].Origins))
}

func TestNamedSQL(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		in, want string
		names    []string
	}{
		"parameters in order": {"VALUES (:a, :b_c)", "VALUES ($1, $2)", []string{"a", "b_c"}},
		"a repeated name": {
			"WHERE x = :a OR y = :a",
			"WHERE x = $1 OR y = $2",
			[]string{"a", "a"},
		},
		"cast is not a name":     {"SELECT :a::int", "SELECT $1::int", []string{"a"}},
		"quoted text is skipped": {"SELECT ':a', :b", "SELECT ':a', $1", []string{"b"}},
		"a bare colon":           {"SELECT 1 :", "SELECT 1 :", nil},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, names := gopackages.NamedSQLForTest(tc.in)
			equals(t, tc.want, got)
			equals(t, tc.names, names)
		})
	}
}
