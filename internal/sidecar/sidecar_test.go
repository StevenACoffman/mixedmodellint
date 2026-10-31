package sidecar_test

import (
	"reflect"
	"testing"

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
