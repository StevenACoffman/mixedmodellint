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

func TestLoadSchemaForms(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		src          string
		wantEntities []string
		wantProblems int
	}{
		"legacy definitions keyword": {
			src: `{"$schema": "http://json-schema.org/draft-07/schema#", "type": "object",
				"properties": {"id": {"type": "integer"}},
				"definitions": {"Item": {"type": "object", "properties": {"sku": {"type": "string"}}}}}`,
			wantEntities: []string{"thing", "Item"},
		},
		"object inferred from properties": {
			src:          `{"$schema": "x", "properties": {"id": {"type": "integer"}}}`,
			wantEntities: []string{"thing"},
		},
		"non-object root is not an entity": {
			src:          `{"$schema": "x", "type": "string"}`,
			wantEntities: nil,
		},
		"boolean property schema": {
			src:          `{"$schema": "x", "type": "object", "properties": {"anything": true}}`,
			wantEntities: []string{"thing"},
		},
		"invalid JSON is a problem": {
			src:          `{"$schema": "x", "type": `,
			wantProblems: 1,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			out := jsonschema.Load(
				[]jsonschema.File{{Path: "thing.schema.json", Src: []byte(tc.src)}},
			)
			var names []string
			for i := range out.Entities {
				names = append(names, out.Entities[i].Name)
			}
			equals(t, tc.wantEntities, names)
			equals(t, tc.wantProblems, len(out.Problems))
		})
	}
}

func TestLoadPropertyShapes(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		prop     string
		wantType mml.LogicalType
		wantRaw  string
	}{
		"one type":          {`{"type": "string"}`, mml.TypeString, "string"},
		"nullable one type": {`{"type": ["string", "null"]}`, mml.TypeString, "string"},
		"a union of kinds": {
			`{"type": ["array", "object", "string"]}`,
			mml.TypeUnknown,
			"array|object|string",
		},
		"oneOf": {
			`{"oneOf": [{"type": "string"}, {"type": "integer"}]}`,
			mml.TypeUnknown,
			"oneOf",
		},
		"anyOf": {
			`{"anyOf": [{"type": "string"}, {"type": "integer"}]}`,
			mml.TypeUnknown,
			"anyOf",
		},
		"no type at all": {`{"description": "anything"}`, mml.TypeUnknown, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			src := `{"$schema": "x", "type": "object", "properties": {"p": ` + tc.prop + `}}`
			out := jsonschema.Load([]jsonschema.File{{Path: "thing.schema.json", Src: []byte(src)}})
			p, _ := out.Entities[0].Field("p")
			equals(t, tc.wantType, p.Type)
			equals(t, tc.wantRaw, p.RawType)
		})
	}
}

func TestLoadMaxItems(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		prop string
		want string
	}{
		"maxItems": {`{"type": "array", "maxItems": 20}`, "20"},
		"x-mml-max-items wins": {
			`{"type": "array", "x-mml-max-items": "50", "maxItems": 20}`,
			"50",
		},
		"x-mml-max-items after": {
			`{"type": "array", "maxItems": 20, "x-mml-max-items": "50"}`,
			"50",
		},
		"an unbounded array has none": {`{"type": "array"}`, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			src := `{"$schema": "x", "type": "object", "properties": {"p": ` + tc.prop + `}}`
			out := jsonschema.Load([]jsonschema.File{{Path: "thing.schema.json", Src: []byte(src)}})
			p, _ := out.Entities[0].Field("p")
			got, _ := p.Annotations.Get("max-items")
			equals(t, tc.want, got)
		})
	}
}
