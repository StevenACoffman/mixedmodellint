package mixedmodellint

import (
	"fmt"
	"slices"
	"strings"
)

func section8Designs() []design {
	return []design{
		{id: "8.2-A", title: "Junction-table hazard tagging", strength: Must, check: check82A},
		{
			id: "8.3-A", title: "Measure-classification contract", strength: Must,
			check: check83A,
		},
		{
			id: "8.4-A", title: "Stored ratio without components", strength: Must,
			heuristic: true, check: check84A,
		},
		{
			id: "8.3-B", title: "SUM over time of a semi-additive measure", strength: Must,
			check: check83B,
		},
		{
			id: "8.4-B", title: "Aggregate-of-aggregate query detector", strength: Must,
			check: check84B,
		},
		{
			id: "8.5-B", title: "Stored-aggregate bound contract", strength: Must,
			check: check85B,
		},
		{id: "8.6-A", title: "Window-output entity contract", strength: Must, check: check86A},
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

// junction reports whether e links two or more entities many-to-many
// (rule_plan.md §8.2-A): at least two references, and either a key equal to
// the union of the reference columns or at most two other columns.
func junction(e *Entity) bool {
	if len(e.Refs) < 2 {
		return false
	}
	var refCols []string
	for _, r := range e.Refs {
		refCols = append(refCols, r.Fields...)
	}
	for _, k := range e.Keys() {
		if sameColumns(k, refCols) {
			return true
		}
	}
	other := 0
	for i := range e.Fields {
		if !containsFold(refCols, e.Fields[i].Name) &&
			!containsFold(e.PrimaryKey, e.Fields[i].Name) {
			other++
		}
	}
	return other <= 2
}

func check82A(c *catalog) []issue {
	var out []issue
	for _, j := range c.entities {
		if !junction(j) {
			continue
		}
		if is, ok := c.junctionIssue(j); ok {
			out = append(out, is)
		}
	}
	return out
}

// junctionIssue reports a junction whose sides carry a measure but which
// does not say how that measure splits across the pairs, or names a weight
// or flag column it does not have.
func (c *catalog) junctionIssue(j *Entity) (issue, bool) {
	policy, declared := j.Annotations.Get("m2m")
	if declared {
		col := policyColumn(policy)
		if _, ok := j.Field(col); col != "" && !ok {
			return issue{
				entity: j,
				measured: fmt.Sprintf("%s declares m2m=%s, but has no column %s", j.Name,
					policy, col),
				suggestion: fmt.Sprintf("Add %s to %s, or correct the m2m declaration (§8.2).",
					col, j.Name),
			}, true
		}
		return issue{}, false
	}
	sides := c.parents(j)
	for _, x := range sides {
		measure, exact := measureField(x)
		if measure == "" {
			continue
		}
		is := junctionHazard(j, x, sides, measure)
		if !exact {
			is.level = SeverityWarning
		}
		return is, true
	}
	return issue{}, false
}

// policyColumn returns the column an m2m policy names: the weight of
// allocated(w) or the flag of primary(f), or "".
func policyColumn(policy string) string {
	lo, hi := strings.IndexByte(policy, '('), strings.LastIndexByte(policy, ')')
	if lo < 0 || hi < lo {
		return ""
	}
	return strings.TrimSpace(policy[lo+1 : hi])
}

// measureField returns a field of x that is summed: one declared
// measure=additive (exact), or else a number named like an amount
// (heuristic).
func measureField(x *Entity) (name string, exact bool) {
	for i := range x.Fields {
		m, _ := x.Fields[i].Annotations.Get("measure")
		if class, _, _ := strings.Cut(m, " "); class == "additive" {
			return x.Fields[i].Name, true
		}
	}
	for i := range x.Fields {
		f := &x.Fields[i]
		if isNumeric(f.Type) && slices.ContainsFunc(splitWords(f.Name), func(w string) bool {
			switch w {
			case "amount", "revenue", "price", "qty", "total":
				return true
			}
			return false
		}) {
			return f.Name, false
		}
	}
	return "", false
}

func junctionHazard(j, x *Entity, sides []*Entity, measure string) issue {
	y := "the other side"
	for _, s := range sides {
		if s != x {
			y = s.Name
			break
		}
	}
	return issue{
		entity: j,
		measured: fmt.Sprintf("%s links %s and %s many-to-many, so summing %s.%s grouped by "+
			"%s counts each %s once per %s and the groups overlap", j.Name, x.Name, y, x.Name,
			measure, y, x.Canonical, singular(y)),
		suggestion: fmt.Sprintf("Declare m2m=overlapping on %s and label reports as "+
			"overlapping; or add allocation_weight NUMERIC NOT NULL CHECK (allocation_weight "+
			"BETWEEN 0 AND 1) summing to 1 per %s and declare m2m=allocated(allocation_weight); "+
			"or add is_primary BOOLEAN with a partial unique index and declare "+
			"m2m=primary(is_primary) (§8.2).", j.Name, x.Canonical),
	}
}

// measureScoped reports whether §8.3-A asks e's numbers for a measure
// class: analytical, feature, wide or event data, or anything with a grain.
func measureScoped(e *Entity) bool {
	layer, _ := e.Annotations.Get("layer")
	switch layer {
	case "analytical", "feature", "obt", "event":
		return true
	default:
		return e.Annotations.Has("grain")
	}
}

func check83A(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		if !measureScoped(e) {
			continue
		}
		for i := range e.Fields {
			f := &e.Fields[i]
			if is, ok := measureClassIssue(e, f); ok {
				out = append(out, is)
			}
		}
	}
	return out
}

// measureClassIssue applies §8.3-A to one field: a number that is not a key
// or a code must declare how it adds up.
func measureClassIssue(e *Entity, f *Field) (issue, bool) {
	if !measureCandidate(e, f) {
		return issue{}, false
	}
	declared, ok := f.Annotations.Get("measure")
	if !ok {
		guess := measureGuess(f.Name)
		return issue{
			entity: e, field: f.Name, pos: f.Pos,
			measured: fmt.Sprintf("%s.%s has no additivity class", e.Name, f.Name),
			suggestion: fmt.Sprintf("Its name suggests %s. Declare measure=%s%s so the query "+
				"checks can protect it (§8.3).", guess, guess, overHint(guess)),
		}, true
	}
	return declaredMeasureIssue(e, f, declared)
}

// declaredMeasureIssue checks a declared measure class: it must be one of
// the three, a semi-additive one must say over what, and one the name
// contradicts gets a note asking for confirmation.
func declaredMeasureIssue(e *Entity, f *Field, declared string) (issue, bool) {
	class, _, _ := strings.Cut(declared, " ")
	guess := measureGuess(f.Name)
	is := issue{entity: e, field: f.Name, pos: f.Pos}
	switch {
	case class == "nominal":
		return issue{}, false // a label, not a quantity (§8.8)
	case class != "additive" && class != "semi_additive" && class != "non_additive":
		is.measured = fmt.Sprintf("%s.%s declares measure=%s, which is not a class", e.Name,
			f.Name, declared)
		is.suggestion = "Use measure=additive, semi_additive (with over=<dimension>), " +
			"non_additive, or nominal for a code (§8.3, §8.8)."
	case class == "semi_additive" && !f.Annotations.Has("over"):
		is.measured = fmt.Sprintf("%s.%s is semi-additive but does not say over which "+
			"dimension it must not be summed", e.Name, f.Name)
		is.suggestion = "Add over=time (or the dimension it must not be summed across) (§8.3)."
	case class != guess && measureLexicon(f.Name) != "":
		is.level = SeverityNote
		is.measured = fmt.Sprintf("%s.%s declares measure=%s, but its name suggests %s",
			e.Name, f.Name, class, guess)
		is.suggestion = "Confirm the class; waive this finding if the declaration is right " +
			"(§8.3)."
	default:
		return issue{}, false
	}
	return is, true
}

// measureCandidate reports whether f is a number that is a quantity rather
// than a key, a reference or a code.
func measureCandidate(e *Entity, f *Field) bool {
	return isNumeric(f.Type) && !isKeyOrRef(e, f.Name) && !nominalName(f.Name) &&
		len(f.Enum) == 0 && !hasEnum(e, f)
}

// nominalName reports whether a name says its field identifies or codes
// something (customer_id, zip_code, sku), so its numbers are labels, not
// quantities (§8.8).
func nominalName(name string) bool {
	n := snakeName(name)
	for _, suffix := range []string{"_id", "_code", "_key", "zip", "postal", "sku", "phone"} {
		if strings.HasSuffix(n, suffix) {
			return true
		}
	}
	return false
}

// measureGuess proposes a class from a field's name, additive when the
// lexicon has no opinion.
func measureGuess(name string) string {
	if g := measureLexicon(name); g != "" {
		return g
	}
	return "additive"
}

// measureLexicon is §8.3-A's English lexicon: a class for a name word, or ""
// when no word decides.
func measureLexicon(name string) string {
	for _, w := range splitWords(name) {
		switch w {
		case "balance", "inventory", "stock", "level", "headcount", "outstanding":
			return "semi_additive"
		case "rate", "ratio", "pct", "percent", "avg", "mean", "median", "margin", "score",
			"price", "p50", "p90", "p95", "p99":
			return "non_additive"
		case "amount", "revenue", "qty", "count", "cost", "fee":
			return "additive"
		}
	}
	if strings.Contains(strings.ToLower(name), "on_hand") {
		return "semi_additive"
	}
	return ""
}

func overHint(class string) string {
	if class == "semi_additive" {
		return " over=time"
	}
	return ""
}

func check85B(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		g, _, _ := c.groupGrain(e)
		layer, _ := e.Annotations.Get("layer")
		if layer != "analytical" && !c.rx.periodGrain.MatchString(g.text) {
			continue
		}
		if gaps := c.boundGaps(e); len(gaps) > 0 {
			out = append(out, issue{
				entity: e,
				measured: fmt.Sprintf("%s stores aggregates but %s", e.Name,
					strings.Join(gaps, "; ")),
				suggestion: boundHint(e, c.periodKeyed(e)),
			})
		}
	}
	return out
}

// boundGaps lists what §8.5-B requires of aggregate entity e that is missing:
// a period in its key (unless it declares period=all_time and has an as_of
// field), and a dimensions= list whose every entry is a key column.
func (c *catalog) boundGaps(e *Entity) []string {
	var gaps []string
	period, _ := e.Annotations.Get("period")
	_, asOf := e.Field("as_of")
	switch {
	case period == "all_time" && !asOf:
		gaps = append(gaps, "declares period=all_time without an as_of field recording "+
			"when it was computed")
	case period != "all_time" && !c.periodKeyed(e):
		gaps = append(gaps, "no key column is a period (a date, period_start and "+
			"period_end, or a reference to dim_date)")
	}
	dims, ok := e.Annotations.Get("dimensions")
	if !ok {
		return append(gaps, "it does not declare dimensions=")
	}
	for _, d := range splitList(dims) {
		if !slices.ContainsFunc(e.Keys(), func(k []string) bool { return containsFold(k, d) }) {
			gaps = append(gaps, fmt.Sprintf("dimension %s is not a key column", d))
		}
	}
	return gaps
}

// periodKeyed reports whether a key of e holds its period: a date or
// timestamp column, both period_start and period_end, or a column
// referencing dim_date.
func (c *catalog) periodKeyed(e *Entity) bool {
	for _, k := range e.Keys() {
		if containsFold(k, "period_start") && containsFold(k, "period_end") {
			return true
		}
		for _, col := range k {
			if f, ok := e.Field(col); ok && isTemporal(f.Type) {
				return true
			}
		}
	}
	return slices.ContainsFunc(e.Refs, func(r Ref) bool {
		target := c.lookup(r.Target, e.Source)
		return target != nil && target.Canonical == "dim_date" &&
			slices.ContainsFunc(e.Keys(), func(k []string) bool { return subsetFold(r.Fields, k) })
	})
}

// boundHint says how to bound aggregate e: by its period, when no key holds
// one, and by the dimensions it is computed over.
func boundHint(e *Entity, periodKeyed bool) string {
	var b strings.Builder
	if !periodKeyed && !e.Annotations.Has("period") {
		fmt.Fprintf(&b, "Add period_start DATE NOT NULL and period_end DATE NOT NULL to the "+
			"key of %s, or reference dim_date; a deliberate all-time aggregate declares "+
			"period=all_time and records as_of. ", e.Name)
	}
	fmt.Fprintf(&b, "Declare dimensions=%s, listing the key columns %s is computed over "+
		"(§8.5).", dimensionHint(e), e.Name)
	return b.String()
}

// dimensionHint proposes a dimensions= value: e's key columns that are not
// temporal, or a placeholder.
func dimensionHint(e *Entity) string {
	var dims []string
	for _, col := range e.PrimaryKey {
		if f, ok := e.Field(col); ok && !isTemporal(f.Type) {
			dims = append(dims, col)
		}
	}
	if len(dims) == 0 {
		return "<key columns>"
	}
	return strings.Join(dims, ",")
}

func check86A(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		_, start := e.Field("window_start")
		_, end := e.Field("window_end")
		if !start || !end {
			continue
		}
		if gaps := c.windowGaps(e); len(gaps) > 0 {
			out = append(out, issue{
				entity: e,
				measured: fmt.Sprintf("%s stores windowed aggregates but %s", e.Name,
					strings.Join(gaps, "; ")),
				suggestion: "Declare window=tumbling(5m), sliding(<size>,<slide>) or " +
					"session(<gap>), lateness=<duration> and late_policy=drop|update|" +
					"side_output (§8.6). A sliding window counts each event once per " +
					"overlapping window, so its measures are measure=non_additive.",
			})
		}
	}
	return out
}

// windowGaps lists what §8.6-A requires of window output e that is missing
// or wrong: a well-formed window, lateness and late_policy, and no additive
// measure in a sliding window.
func (c *catalog) windowGaps(e *Entity) []string {
	var gaps []string
	if missing := undeclared(e.Annotations, "window", "lateness", "late_policy"); len(missing) > 0 {
		gaps = append(gaps, "it does not declare "+strings.Join(missing, ", "))
	}
	window, hasWindow := e.Annotations.Get("window")
	if hasWindow && !c.rx.windowSpec.MatchString(window) {
		gaps = append(gaps, fmt.Sprintf("window=%s is not tumbling(<size>), "+
			"sliding(<size>,<slide>) or session(<gap>)", window))
	}
	policy, hasPolicy := e.Annotations.Get("late_policy")
	if hasPolicy && !slices.Contains([]string{"drop", "update", "side_output"}, policy) {
		gaps = append(gaps, fmt.Sprintf("late_policy=%s is not drop, update or side_output",
			policy))
	}
	if strings.HasPrefix(window, "sliding(") {
		gaps = append(gaps, slidingAdditive(e)...)
	}
	return gaps
}

// undeclared returns, as key=, each of keys that a does not declare, in
// the order given.
func undeclared(a Annotations, keys ...string) []string {
	var out []string
	for _, k := range keys {
		if !a.Has(k) {
			out = append(out, k+"=")
		}
	}
	return out
}

// slidingAdditive reports each measure of e declared additive, which a
// sliding window's overlap makes false.
func slidingAdditive(e *Entity) []string {
	var out []string
	for i := range e.Fields {
		if m, _ := e.Fields[i].Annotations.Get("measure"); m == "additive" {
			out = append(out, fmt.Sprintf("its window slides, so %s is counted once per "+
				"overlapping window and cannot be measure=additive", e.Fields[i].Name))
		}
	}
	return out
}
