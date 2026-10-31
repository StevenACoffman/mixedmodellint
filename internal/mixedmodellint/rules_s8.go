package mixedmodellint

import (
	"fmt"
	"slices"
	"strings"
)

func section8Designs() []design {
	return []design{
		{
			id: "8.4-A", title: "Stored ratio without components", strength: Must,
			heuristic: true, check: check84A,
		},
	}
}

// nonDecomposableWord reports whether a name word marks a value that cannot
// be summed or averaged again: an average, a ratio, a percentile, or a
// distinct count.
func nonDecomposableWord(w string) bool {
	switch w {
	case "avg", "average", "mean", "rate", "ratio", "pct", "percent", "share", "margin",
		"median", "p50", "p90", "p95", "p99", "percentile", "unique", "distinct",
		"dau", "wau", "mau":
		return true
	default:
		return false
	}
}

func check84A(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		g, _, _ := c.groupGrain(e)
		layer, _ := e.Annotations.Get("layer")
		if layer != "analytical" && !c.rx.coarseGrain.MatchString(g.text) {
			continue // atomic grain: a stored per-row ratio is legitimate
		}
		for i := range e.Fields {
			f := &e.Fields[i]
			if is, ok := storedRatio(e, f, g.text); ok {
				out = append(out, is)
			}
		}
	}
	return out
}

// storedRatio reports f when it holds a non-decomposable value with neither
// its additive components nor a mergeable sketch. A field declared
// measure=non_additive is reported at full severity (an error); a match on
// name alone is the design's heuristic severity.
func storedRatio(e *Entity, f *Field, grain string) (issue, bool) {
	measure, _ := f.Annotations.Get("measure")
	declared := strings.HasPrefix(measure, "non_additive")
	named := slices.ContainsFunc(splitWords(f.Name), nonDecomposableWord)
	if !declared && !named || !isNumeric(f.Type) {
		return issue{}, false
	}
	if r, _ := f.Annotations.Get("reaggregate"); r == "forbidden" {
		return issue{}, false
	}
	if hasComponents(e, f) || hasSketch(e, f) {
		return issue{}, false
	}
	is := issue{
		entity: e, field: f.Name, pos: f.Pos,
		measured: fmt.Sprintf("%s.%s stores a non-decomposable value at grain %q with no "+
			"numerator/denominator or mergeable sketch beside it", e.Name, f.Name, grain),
		suggestion: fmt.Sprintf("Store the additive parts as columns and compute "+
			"SUM(<numerator>)::numeric / NULLIF(SUM(<denominator>), 0) at query time; the "+
			"::numeric cast avoids integer division. Declare them with measure=non_additive;"+
			"numerator=<col>;denominator=<col> on %s. For a distinct count or percentile, "+
			"recount from atomic data or store a sketch (%s_hll) with approx_error=.",
			f.Name, stemWithout(f.Name)),
	}
	if declared {
		is.level = SeverityError
	}
	return is, true
}

func isNumeric(t LogicalType) bool {
	return t == TypeInt || t == TypeDecimal || t == TypeFloat
}

// hasComponents reports whether f's numerator and denominator exist on e,
// either declared (numerator=, denominator=) or by name: a *_sum or *_total
// field and a *_count field.
func hasComponents(e *Entity, f *Field) bool {
	num, okN := f.Annotations.Get("numerator")
	den, okD := f.Annotations.Get("denominator")
	if okN && okD {
		_, hasN := e.Field(num)
		_, hasD := e.Field(den)
		return hasN && hasD
	}
	var sum, count bool
	for i := range e.Fields {
		other := &e.Fields[i]
		name := strings.ToLower(other.Name)
		sum = sum || hasAnySuffix(name, "_sum", "_total")
		count = count || strings.HasSuffix(name, "_count")
	}
	return sum && count
}

// hasSketch reports a mergeable sketch for f: an hll or tdigest column, or a
// column named <stem>_hll/_sketch/_tdigest, with the accepted error recorded.
func hasSketch(e *Entity, f *Field) bool {
	stem := stemWithout(f.Name)
	for i := range e.Fields {
		other := &e.Fields[i]
		raw := strings.ToLower(other.RawType)
		name := strings.ToLower(other.Name)
		isSketch := raw == "hll" || raw == "tdigest" ||
			strings.HasPrefix(name, stem) && hasAnySuffix(name, "_hll", "_sketch", "_tdigest")
		if isSketch && other.Annotations.Has("approx_error") {
			return true
		}
	}
	return false
}

// stemWithout returns a name without its non-decomposable words:
// avg_order_value → order_value.
func stemWithout(name string) string {
	var kept []string
	for _, w := range splitWords(name) {
		if !nonDecomposableWord(w) {
			kept = append(kept, w)
		}
	}
	return strings.Join(kept, "_")
}
