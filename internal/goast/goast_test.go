package goast_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/StevenACoffman/mixedmodellint/internal/goast"
	mml "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

func equals(t *testing.T, exp, act any) {
	t.Helper()
	if !reflect.DeepEqual(exp, act) {
		t.Fatalf("expected %#v, got %#v", exp, act)
	}
}

func TestLoad(t *testing.T) {
	t.Parallel()
	p := filepath.Join("testdata", "model", "order.go")
	src, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	out := goast.Load([]goast.File{{Path: p, Src: src}})
	byName := map[string]mml.Entity{}
	for i := range out.Entities {
		byName[out.Entities[i].Name] = out.Entities[i]
	}
	order, line := byName["Order"], byName["OrderLine"]
	var orderFields []string
	for i := range order.Fields {
		orderFields = append(orderFields, order.Fields[i].Name)
	}
	grain, _ := order.Annotations.Get("grain")
	status, _ := order.Field("status")
	null, _ := status.Annotations.Get("null")
	updated, _ := order.Field("updated_at")
	note, _ := line.Field("note")

	cases := map[string]struct{ exp, act any }{
		"tagged structs only": {2, len(out.Entities)},
		"embedded fields expanded, - skipped": {
			[]string{"id", "customer_id", "status", "created_at", "updated_at"},
			orderFields,
		},
		"directive read":        {"one row per order", grain},
		"field tag annotation":  {"forbidden", null},
		"id is the default key": {[]string{"id"}, order.PrimaryKey},
		"mml pk tags":           {[]string{"order_id", "line_no"}, line.PrimaryKey},
		"pointer is nullable":   {true, updated.Nullable},
		"time.Time is zoned":    {mml.TypeTimestamptz, updated.Type},
		"omitempty is nullable": {true, note.Nullable},
		"inferred ref by name":  {"Order", line.Refs[0].Target},
		"fixed array raw type":  {"[3]string", line.Fields[2].RawType},
		"position at type name": {15, order.Pos.Line},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.exp, tc.act)
		})
	}
}
