package mixedmodellint_test

import (
	"strings"
	"testing"

	"github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

func TestParseLines(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		text string
		key  string
		want string
		line int
	}{
		"sql comment":     {"Orders.\n@grain: one row per order", "grain", "one row per order", 11},
		"go directive":    {"mml:layer event", "layer", "event", 10},
		"todo is missing": {"@grain: TODO", "grain", "", 0},
		"prose ignored":   {"email me @ noon", "email", "", 0},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			anns := mixedmodellint.ParseLines(
				tc.text,
				mixedmodellint.Position{File: "a.sql", Line: 10},
			)
			got, _ := anns.Get(tc.key)
			equals(t, tc.want, got)
			if tc.line > 0 {
				equals(t, tc.line, anns[tc.key].Pos.Line)
			}
		})
	}
}

func TestParseTag(t *testing.T) {
	t.Parallel()
	anns := mixedmodellint.ParseTag("null=not_applicable; unit=USD;pk", mixedmodellint.Position{})
	cases := map[string]struct{ key, want string }{
		"value": {"null", "not_applicable"},
		"space": {"unit", "USD"},
		"flag":  {"pk", "true"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, _ := anns.Get(tc.key)
			equals(t, tc.want, got)
		})
	}
}

func TestParseLinesKeySyntax(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		line  string
		key   string
		value string
		found bool
	}{
		"hyphenated key":         {"@max-items: 3", "max-items", "3", true},
		"underscore key":         {"@natural_key: email", "natural_key", "email", true},
		"digit inside key":       {"@p95: 120", "p95", "120", true},
		"leading digit rejected": {"@1bad: x", "1bad", "", false},
		"space in key rejected":  {"@a b: x", "a b", "", false},
		"empty key rejected":     {"@: x", "", "", false},
		"no colon rejected":      {"@grain one row", "grain one row", "", false},
		"directive no value":     {"mml:pk", "pk", "", true},
		"directive value spaces": {
			"mml:grain  one row per order ",
			"grain",
			"one row per order",
			true,
		},
		"indented line":               {"   @unit: USD", "unit", "USD", true},
		"symbol in key rejected":      {"@a$b: x", "a$b", "", false},
		"leading hyphen rejected":     {"@-x: v", "-x", "", false},
		"trailing hyphen allowed":     {"@x-: v", "x-", "v", true},
		"leading underscore rejected": {"@_x: v", "_x", "", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			anns := mixedmodellint.ParseLines(tc.line, mixedmodellint.Position{})
			a, found := anns[tc.key]
			equals(t, tc.found, found)
			equals(t, tc.value, a.Value)
			if !tc.found {
				equals(t, 0, len(anns))
			}
		})
	}
}

func TestParseTagEdges(t *testing.T) {
	t.Parallel()
	anns := mixedmodellint.ParseTag(";; pk ; unit = USD ;", mixedmodellint.Position{Line: 4})
	equals(t, 2, len(anns))
	equals(t, "true", anns["pk"].Value)
	equals(t, "USD", anns["unit"].Value)
	equals(t, 4, anns["unit"].Pos.Line)
	equals(t, 0, len(mixedmodellint.ParseTag("", mixedmodellint.Position{})))
}

// TestGrainParams checks the parameter syntax of an annotation value through
// a grain's fp= fingerprint, the parameter whose value the linter compares.
func TestGrainParams(t *testing.T) {
	t.Parallel()
	cart := func(grain string) mixedmodellint.Entity {
		e := mixedmodellint.Entity{
			ID: "sql:carts", Kind: mixedmodellint.KindTable, Source: mixedmodellint.SourceSQL,
			Name: "carts", PrimaryKey: []string{"cart_id"},
			Fields: []mixedmodellint.Field{{Name: "cart_id", Type: mixedmodellint.TypeInt}},
		}
		if grain != "" {
			e.Annotations = mixedmodellint.Annotations{"grain": {Value: grain}}
		}
		return e
	}
	designs := func(e mixedmodellint.Entity) (ids []string, suggestion string) {
		r := mixedmodellint.Analyze(&mixedmodellint.Input{
			Sources: []mixedmodellint.Source{{Entities: []mixedmodellint.Entity{e}}},
		})
		for i := range r.Findings {
			if r.Findings[i].Rule == "7.1" { // the grain designs read the parameters
				ids = append(ids, r.Findings[i].Design)
				suggestion = r.Findings[i].Suggestion
			}
		}
		return ids, suggestion
	}
	// The missing-grain suggestion carries the current fingerprint.
	_, hint := designs(cart(""))
	_, after, _ := strings.Cut(hint, "fp=")
	fp := strings.TrimRight(after, "'.") // the SQL hint ends with a quote and a period
	equals(t, 8, len(fp))

	cases := map[string]struct {
		grain string
		want  []string
	}{
		"bare fp":                   {"one row per cart fp=" + fp, nil},
		"double-quoted fp":          {`one row per cart fp="` + fp + `"`, nil},
		"single-quoted fp":          {"one row per cart fp='" + fp + "'", nil},
		"extra spaces":              {"one row per cart   fp=" + fp + "  ", nil},
		"unterminated quote stale":  {`one row per cart fp="` + fp, []string{"7.1-A"}},
		"mismatched quotes stale":   {`one row per cart fp="` + fp + "'", []string{"7.1-A"}},
		"quoted param with spaces":  {`one row per cart note="a b c"`, nil},
		"stale fp":                  {"one row per cart fp=00000000", []string{"7.1-A"}},
		"todo in any case missing":  {"todo", []string{"7.1-A"}},
		"blank is missing":          {"   ", []string{"7.1-A"}},
		"lone quote is not a param": {`one row per cart "`, []string{"7.1-A"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, _ := designs(cart(tc.grain))
			equals(t, tc.want, got)
		})
	}
}

func TestParseLinesPositions(t *testing.T) {
	t.Parallel()
	text := "intro\n@grain: one row per order\n@unit: USD"
	cases := map[string]struct {
		start, wantGrain, wantUnit int
	}{
		"offset from start line": {10, 11, 12},
		"unknown line stays 0":   {0, 0, 0},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			anns := mixedmodellint.ParseLines(
				text,
				mixedmodellint.Position{File: "a.sql", Line: tc.start},
			)
			equals(t, tc.wantGrain, anns["grain"].Pos.Line)
			equals(t, tc.wantUnit, anns["unit"].Pos.Line)
		})
	}
}
