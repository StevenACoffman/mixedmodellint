package jsonschema_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/StevenACoffman/mixedmodellint/internal/jsonschema"
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
	p := filepath.Join("testdata", "schemas", "order.schema.json")
	src, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	out := jsonschema.Load([]jsonschema.File{{Path: p, Src: src}})
	order, line := out.Entities[0], out.Entities[1]
	grain, _ := order.Annotations.Get("grain")
	placed, _ := order.Field("placed_at")
	shipped, _ := order.Field("shipped_on")
	coupon, _ := order.Field("coupon")
	null, _ := coupon.Annotations.Get("null")

	cases := map[string]struct{ exp, act any }{
		"root and object defs only":   {2, len(out.Entities)},
		"title names the root":        {"Order", order.Name},
		"def key names a def":         {"Line", line.Name},
		"x-mml annotation":            {"one row per order", grain},
		"x-mml-key is the key":        {[]string{"order_id"}, order.PrimaryKey},
		"date-time is zoned":          {mml.TypeTimestamptz, placed.Type},
		"required is not nullable":    {false, placed.Nullable},
		"plain string stays a string": {mml.TypeString, shipped.Type},
		"null in type is nullable":    {true, coupon.Nullable},
		"property annotation":         {"not_applicable", null},
		"array items ref":             {"Line", order.Refs[0].Target},
		"property position":           {10, placed.Pos.Line},
		"def position":                {16, line.Pos.Line},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.exp, tc.act)
		})
	}
}

func TestIsSchema(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		path string
		src  string
		want bool
	}{
		"schema suffix": {"a.schema.json", "{}", true},
		"$schema key":   {"a.json", `{"$schema": "x"}`, true},
		"package.json":  {"package.json", `{"name": "x"}`, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.want, jsonschema.IsSchema(tc.path, []byte(tc.src)))
		})
	}
}
