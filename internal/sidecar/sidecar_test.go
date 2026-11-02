package sidecar_test

import (
	"reflect"
	"testing"

	mml "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
	"github.com/StevenACoffman/mixedmodellint/internal/sidecar"
)

func equals(t *testing.T, exp, act any) {
	t.Helper()
	if !reflect.DeepEqual(exp, act) {
		t.Fatalf("expected %#v, got %#v", exp, act)
	}
}

func TestParse(t *testing.T) {
	t.Parallel()
	src := []byte(`config:
  ignored: true
entities:
  orders:
    grain: one row per order
    synonyms: [purchase, sale]
    fields:
      amount:
        unit: USD
  carts:
    owner: team-carts
`)
	sc, problems := sidecar.Parse("mml.yaml", src)
	orders := sc.Entities["orders"]
	cases := map[string]struct{ exp, act any }{
		"no problems":      {0, len(problems)},
		"two entities":     {2, len(sc.Entities)},
		"scalar":           {"one row per order", orders.Annotations["grain"].Value},
		"sequence joined":  {"purchase, sale", orders.Annotations["synonyms"].Value},
		"field annotation": {"USD", orders.Fields["amount"]["unit"].Value},
		"position":         {5, orders.Annotations["grain"].Pos.Line},
		"one-key mapping":  {"team-carts", sc.Entities["carts"].Annotations["owner"].Value},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.exp, tc.act)
		})
	}
}

func TestParseInvalid(t *testing.T) {
	t.Parallel()
	_, problems := sidecar.Parse("mml.yaml", []byte("entities: [unclosed\n"))
	equals(t, 1, len(problems))
}

func TestParseLogical(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		src      string
		want     []mml.LogicalRecord
		problems int
	}{
		"flow and block sequences": {
			src: "entity: order_line\nattributes: [order_id, amount]\nkeys:\n  - order_id\n  - line_no\n",
			want: []mml.LogicalRecord{
				{
					Entity:     "order_line",
					Attributes: []string{"order_id", "amount"},
					Keys: []string{
						"order_id",
						"line_no",
					},
					Pos: mml.Position{File: "l.yaml", Line: 1, Col: 1},
				},
			},
		},
		"single key as a scalar": {
			src: "entity: cart\nkeys: cart_id\n",
			want: []mml.LogicalRecord{
				{
					Entity: "cart",
					Keys:   []string{"cart_id"},
					Pos:    mml.Position{File: "l.yaml", Line: 1, Col: 1},
				},
			},
		},
		"no entity":  {src: "attributes: [a]\n", problems: 1},
		"not YAML":   {src: "entity: [", problems: 1},
		"empty file": {src: ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, problems := sidecar.ParseLogical("l.yaml", []byte(tc.src))
			equals(t, tc.want, got)
			equals(t, tc.problems, len(problems))
		})
	}
}

func TestParseOwnership(t *testing.T) {
	t.Parallel()
	sidecarSrc := `owners:
  team:orders:
    model: [orders, order_*]
    responsibilities: [definitions, data_quality]
    decisions:
      - {name: approve_breaking_change, without_signoff: true}
      - deprecate
    granted_by: role:vp-data
  team:growth:
    ownership: liability
entities: {}
`
	recs, problems := sidecar.ParseOwnership("mml.yaml", []byte(sidecarSrc), "")
	equals(t, 0, len(problems))
	equals(t, 2, len(recs))
	equals(t, mml.OwnershipRecord{
		Owner: "team:orders", Model: []string{"orders", "order_*"},
		Responsibilities: []string{"definitions", "data_quality"},
		Decisions: []mml.Decision{
			{Name: "approve_breaking_change", WithoutSignoff: true}, {Name: "deprecate"},
		},
		GrantedBy: "role:vp-data", Pos: mml.Position{File: "mml.yaml", Line: 2, Col: 3},
	}, recs[0])
	equals(t, true, recs[1].Liability)

	file, problems := sidecar.ParseOwnership("model/ownership/team-x.yaml",
		[]byte("model: [carts]\ngranted_by: role:cto\n"), "team-x")
	equals(t, 0, len(problems))
	equals(t, "team-x", file[0].Owner)
	equals(t, []string{"carts"}, file[0].Model)
	equals(t, mml.Position{File: "model/ownership/team-x.yaml", Line: 1}, file[0].Pos)

	_, problems = sidecar.ParseOwnership("o.yaml", []byte("model: ["), "x")
	equals(t, 1, len(problems))
	none, _ := sidecar.ParseOwnership("o.yaml", nil, "x")
	equals(t, 0, len(none))
}

func TestRenderStubs(t *testing.T) {
	t.Parallel()
	out, err := sidecar.RenderStubs([]mml.Stub{
		{Entity: "order", Key: "grain", Value: "one row per order fp=1a2b"},
		{Entity: "order", Field: "total", Key: "measure", Value: "additive"},
		{Entity: "order", Field: "total", Key: "null", Value: "TODO"},
		{Entity: "order", Field: "note", Key: "null", Value: "TODO"},
		{Entity: "sale", Key: "owner", Value: "TODO"},
	})
	if err != nil {
		t.Fatal(err)
	}
	equals(t, `entities:
  order:
    grain: one row per order fp=1a2b
    fields:
      total:
        measure: additive
        "null": TODO
      note:
        "null": TODO
  sale:
    owner: TODO
`, string(out))
	// The fragment reads back as a sidecar with the same annotations.
	sc, problems := sidecar.Parse("mml.yaml", out)
	equals(t, 0, len(problems))
	equals(t, "additive", sc.Entities["order"].Fields["total"]["measure"].Value)
	equals(t, "TODO", sc.Entities["order"].Fields["total"]["null"].Value)
	empty, err := sidecar.RenderStubs(nil)
	if err != nil {
		t.Fatal(err)
	}
	equals(t, "entities: {}\n", string(empty))
}

func TestQuotedKeys(t *testing.T) {
	t.Parallel()
	sc, problems := sidecar.Parse(
		"mml.yaml",
		[]byte(
			"entities:\n  \"orders\":\n    fields:\n      note:\n        \"null\": missing\n        'card': 0..1\n",
		),
	)
	equals(t, 0, len(problems))
	equals(t, "missing", sc.Entities["orders"].Fields["note"]["null"].Value)
	equals(t, "0..1", sc.Entities["orders"].Fields["note"]["card"].Value)
}
