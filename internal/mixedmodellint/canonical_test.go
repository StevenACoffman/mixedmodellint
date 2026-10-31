package mixedmodellint_test

import (
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

func TestCanonical(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		in, want string
	}{
		"plural table":     {"order_lines", "order_line"},
		"go struct":        {"OrderLine", "order_line"},
		"kebab":            {"Order-Line", "order_line"},
		"ies plural":       {"categories", "category"},
		"sses plural":      {"addresses", "address"},
		"uses plural":      {"statuses", "status"},
		"already singular": {"status", "status"},
		"acronym":          {"HTTPServerID", "http_server_id"},
		"affix dropped":    {"OrderDTO", "order"},
		"plural affix":     {"order_models", "order"},
		"affix only kept":  {"Model", "model"},
		"table affix":      {"orders_tbl", "order"},
		"zes plural":       {"quizzes", "quiz"},
		"irregular plural": {"people", "person"},
		"latin plural":     {"indices", "index"},
		"lone s kept":      {"s", "s"},
		"xes plural":       {"boxes", "box"},
		"ches plural":      {"matches", "match"},
		"shes plural":      {"wishes", "wish"},
		"is ending":        {"analysis", "analysis"},
		"short word":       {"bus", "bus"},
		"record affix":     {"OrderRecord", "order"},
		"entity affix":     {"UserEntity", "user"},
		"digits stay":      {"line2s", "line2"},
		"single letter":    {"x", "x"},
		"empty":            {"", ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.want, mixedmodellint.Canonical(tc.in))
		})
	}
}

func TestCanonicalIdempotent(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		s := rapid.String().Draw(t, "name")
		once := mixedmodellint.Canonical(s)
		if twice := mixedmodellint.Canonical(once); twice != once {
			t.Fatalf("Canonical(%q) = %q, but Canonical(%q) = %q", s, once, once, twice)
		}
	})
}

// TestCanonicalCaseStyle checks that the same words written in snake_case,
// camelCase, PascalCase and kebab-case link to one entity.
func TestCanonicalCaseStyle(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		words := rapid.SliceOfN(rapid.StringMatching(`[a-z]{2,8}`), 1, 4).Draw(t, "words")
		pascal := make([]string, len(words))
		for i, w := range words {
			pascal[i] = strings.ToUpper(w[:1]) + w[1:]
		}
		camel := words[0] + strings.Join(pascal[1:], "")
		styles := []string{
			strings.Join(words, "_"), strings.Join(words, "-"),
			strings.Join(pascal, ""), camel,
		}
		want := mixedmodellint.Canonical(styles[0])
		for _, s := range styles[1:] {
			if got := mixedmodellint.Canonical(s); got != want {
				t.Fatalf("Canonical(%q) = %q, want %q (from %q)", s, got, want, styles[0])
			}
		}
	})
}
