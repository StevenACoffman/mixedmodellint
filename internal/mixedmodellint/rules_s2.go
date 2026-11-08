package mixedmodellint

import (
	"fmt"
	"slices"
	"strings"
)

// Thresholds of §2.5-A. rule_plan.md makes them metadata.* settings; until
// mml.yaml has a config section they are fixed.
const (
	minDescriptionWords = 4
	minFieldCoverage    = 0.9
	maxNameOverlap      = 0.8
)

// fieldMetadata is what the forms of one logical entity say about one
// field, linked by canonical name.
type fieldMetadata struct {
	name      string
	described bool
	measure   bool
	unit      bool
}

func section2Designs() []design {
	return []design{
		{
			id: "2.2-A", title: "Boundary-crossing translation contract", strength: Must,
			check: check22A,
		},
		{
			id: "2.3-A", title: "Classification-field type check", strength: Should,
			check: check23A,
		},
		{id: "2.5-A", title: "Metadata coverage and quality", strength: Should, check: check25A},
	}
}

// check22A reports each declared reference from one domain into another that
// records no agreement on what the shared term means there.
func check22A(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		for i := range e.Refs {
			if is, ok := c.crossing(e, &e.Refs[i]); ok {
				out = append(out, is)
			}
		}
	}
	return out
}

// crossing applies §2.2-A to one reference: it reports a reference that
// crosses a domain boundary which nothing accounts for.
func (c *catalog) crossing(e *Entity, r *Ref) (issue, bool) {
	t, from, to, ok := c.crossesBoundary(e, r)
	if !ok || c.accountedFor(e, r, t) {
		return issue{}, false
	}
	pos := r.Pos
	if f, ok := e.Field(r.Fields[0]); ok && pos == (Position{}) {
		pos = f.Pos
	}
	return issue{
		entity: e, field: strings.Join(r.Fields, ", "), pos: pos,
		measured: fmt.Sprintf("%s (%s) references %s (%s) directly, with no recorded "+
			"agreement on what %q means in both domains", e.Name, from, t.Name, to, t.Canonical),
		suggestion: fmt.Sprintf("Record the agreed meaning of %q on the reference "+
			"(mml:\"mapping=<ADR link>\", x-mml-mapping, or @mapping: in a column "+
			"comment), or route it through a mapping entity %s_%s_map annotated "+
			"translates from=%s to=%s (§2.2).", t.Canonical, from, t.Canonical, from, to),
	}, true
}

// crossesBoundary returns the entity r references and the domains on each
// side when they differ. Inferred references are skipped, since a guessed
// customer_id is §6.2's problem first.
func (c *catalog) crossesBoundary(e *Entity, r *Ref) (t *Entity, from, to string, ok bool) {
	if r.Inferred {
		return nil, "", "", false
	}
	from, t = domainOf(e), c.lookup(r.Target, e.Source)
	if from == "" || t == nil {
		return nil, "", "", false
	}
	to = domainOf(t)
	return t, from, to, to != "" && to != from
}

// accountedFor reports whether a crossing from e to t through r is accounted
// for: the reference records its mapping, either side exists to translate,
// or t is a taxonomy every domain shares outright (§2.3).
func (c *catalog) accountedFor(e *Entity, r *Ref, t *Entity) bool {
	return mapped(e, r) || c.translation(e) || c.translation(t) || c.sharedTaxonomy(t)
}

// mapped reports whether a field of reference r records its boundary mapping.
func mapped(e *Entity, r *Ref) bool {
	for _, name := range r.Fields {
		if f, ok := e.Field(name); ok && f.Annotations.Has("mapping") {
			return true
		}
	}
	return false
}

// translation reports whether e exists to translate between domains: it is
// declared so, or it is a *_map or *_xref table whose references reach two
// domains.
func (c *catalog) translation(e *Entity) bool {
	if e.Annotations.Has("translates") {
		return true
	}
	canon := Canonical(e.Name)
	if !strings.HasSuffix(canon, "_map") && !strings.HasSuffix(canon, "_xref") {
		return false
	}
	domains := map[string]bool{}
	for _, p := range c.parents(e) {
		domains[domainOf(p)] = true
	}
	return len(domains) >= 2
}

// sharedTaxonomy reports whether t is a classification every domain shares
// outright, which §2.2 leaves to §2.3: declared a taxonomy, or a reference
// table whose rows form a hierarchy.
func (c *catalog) sharedTaxonomy(t *Entity) bool {
	switch {
	case t.Annotations.Has("taxonomy"):
		return true
	case !isTaxonomy(t):
		// Neither declared a taxonomy nor a reference table.
		return false
	}
	for _, r := range t.Refs {
		if c.lookup(r.Target, t.Source) == t {
			return true
		}
	}
	return false
}

// check23A reports classification fields stored as free text. A taxonomy's
// own fields are its nodes' labels, not classifications of something else,
// so they are skipped.
func check23A(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		if foreignSchema(e) || isTaxonomy(e) {
			continue
		}
		for i := range e.Fields {
			if is, ok := c.freeTextClassification(e, &e.Fields[i]); ok {
				out = append(out, is)
			}
		}
	}
	return out
}

// freeTextClassification applies §2.3-A to one field: a string named for a
// category, region, tier and the like, with no reference, enum or CHECK tying
// its values to a taxonomy.
func (c *catalog) freeTextClassification(e *Entity, f *Field) (issue, bool) {
	term, ok := c.classificationTerm(f)
	if !ok || f.Type != TypeString || len(enumValues(e, f)) > 0 ||
		c.declaresReference(e, f.Name) {
		return issue{}, false
	}
	target := "a shared " + term + " taxonomy"
	if t := c.lookup(term, e.Source); t != nil && isTaxonomy(t) {
		target = t.Name + " (taxonomy found)"
	}
	return issue{
		entity: e, field: f.Name, pos: f.Pos,
		measured: fmt.Sprintf("%s.%s stores a %s as free text, so every writer can "+
			"invent its own values", e.Name, f.Name, term),
		suggestion: fmt.Sprintf("Replace it with %s_id REFERENCES %s, or constrain it "+
			"to the taxonomy's node list with an enum or CHECK (§2.3).", term, target),
	}, true
}

// classificationTerm returns the classification word f is named after, such
// as "region" for sales_region or SalesRegion.
func (c *catalog) classificationTerm(f *Field) (string, bool) {
	for _, w := range splitWords(f.Name) {
		if c.rx.classification[w] {
			return w, true
		}
	}
	return "", false
}

// isTaxonomy reports whether e holds a taxonomy's nodes: declared so, or a
// reference table.
func isTaxonomy(e *Entity) bool {
	layer, _ := e.Annotations.Get("layer")
	return e.Annotations.Has("taxonomy") || layer == "reference"
}

// check25A reports published entities whose business metadata is
// incomplete: no validation status, too few columns described, or measures
// with no unit. The definition and owner it also asks for are §1.2-A's to
// report, for the same entities, so they are not repeated here. All forms of
// one logical entity are judged together, since a column described in the
// Go struct is described, and reported once, on the first form.
func check25A(c *catalog) []issue {
	var out []issue
	seen := map[string]bool{}
	for _, e := range c.entities {
		if seen[e.Canonical] || !inMetadataScope(e) {
			continue
		}
		seen[e.Canonical] = true
		forms := slices.DeleteFunc(slices.Clone(c.byCanonical[e.Canonical]),
			func(m *Entity) bool { return !inMetadataScope(m) })
		if gaps, example := c.metadataGaps(forms); len(gaps) > 0 {
			out = append(out, issue{
				entity: e,
				measured: fmt.Sprintf("the business metadata of %s is incomplete: %s", e.Name,
					strings.Join(gaps, "; ")),
				suggestion: fmt.Sprintf("Declare status (draft, validated or deprecated), "+
					"describe what each column means in more than its name, and give measures "+
					"a unit or currency, e.g. COMMENT ON COLUMN %s.%s IS '<meaning>, in <unit>. "+
					"@unit: USD' (§2.5).", e.Name, example),
			})
		}
	}
	return out
}

// inMetadataScope reports whether e is in §2.5's scope: published, or of
// undeclared lifecycle and not scratch, owned, and stored rather than a view.
func inMetadataScope(e *Entity) bool {
	life, declared := e.Annotations.Get("lifecycle")
	layer, _ := e.Annotations.Get("layer")
	return e.Kind != KindView && !foreignSchema(e) && layer != "scratch" &&
		(life == "published" || !declared)
}

// metadataGaps lists what forms, together, lack, and names a field to use in
// the suggestion's example.
func (c *catalog) metadataGaps(forms []*Entity) (gaps []string, example string) {
	if g := statusGap(forms); g != "" {
		gaps = append(gaps, g)
	}
	fields := c.fieldMetadata(forms)
	var undescribed, unitless []string
	for _, f := range fields {
		if !f.described {
			undescribed = append(undescribed, f.name)
		}
		if f.measure && !f.unit {
			unitless = append(unitless, f.name)
		}
	}
	covered := 1 - float64(len(undescribed))/float64(max(len(fields), 1))
	if covered < minFieldCoverage {
		gaps = append(gaps, fmt.Sprintf("%.0f%% of columns are described (need %.0f%%), "+
			"missing: %s", covered*100, minFieldCoverage*100, strings.Join(undescribed, ", ")))
	}
	if len(unitless) > 0 {
		gaps = append(gaps, "no unit or currency on "+strings.Join(unitless, ", "))
	}
	example = "<column>"
	switch {
	case len(undescribed) > 0:
		example = undescribed[0]
	case len(unitless) > 0:
		example = unitless[0]
	}
	return gaps, example
}

// statusGap says what is wrong with the validation status the forms
// declare, or "" when one declares draft, validated or deprecated.
func statusGap(forms []*Entity) string {
	for _, e := range forms {
		switch status, _ := e.Annotations.Get("status"); status {
		case "":
			continue
		case "draft", "validated", "deprecated":
			return ""
		default:
			return fmt.Sprintf("status %q is not draft, validated or deprecated", status)
		}
	}
	return "no status"
}

// fieldMetadata merges the fields of forms by canonical name, in the order
// they first appear.
func (c *catalog) fieldMetadata(forms []*Entity) []fieldMetadata {
	var out []fieldMetadata
	index := map[string]int{}
	for _, e := range forms {
		for i := range e.Fields {
			f := &e.Fields[i]
			canon := Canonical(f.Name)
			j, ok := index[canon]
			if !ok {
				j = len(out)
				index[canon] = j
				out = append(out, fieldMetadata{name: f.Name})
			}
			m := &out[j]
			m.described = m.described || describes(f.Doc, f.Name)
			m.measure = m.measure || c.isMeasure(f)
			m.unit = m.unit || f.Annotations.Has("unit") || f.Annotations.Has("currency")
		}
	}
	return out
}

// isMeasure reports whether f is a quantity whose number means nothing
// without a unit: numeric and named like an amount, price or length. An
// identifier such as cost_center_id is not one.
func (c *catalog) isMeasure(f *Field) bool {
	return isNumeric(f.Type) && c.rx.measureName.MatchString(f.Name) &&
		!strings.HasSuffix(Canonical(f.Name), "_id")
}

// describes reports whether doc says something about a field named name: at
// least minDescriptionWords words once annotation lines are removed, and not
// merely the name's own words (Jaccard overlap below maxNameOverlap).
func describes(doc, name string) bool {
	var words []string
	for line := range strings.Lines(doc) {
		if _, _, ok := parseLine(strings.TrimSpace(line)); !ok {
			words = append(words, splitWords(line)...)
		}
	}
	if len(words) < minDescriptionWords {
		return false
	}
	return jaccard(words, splitWords(name)) < maxNameOverlap
}

// jaccard is the Jaccard index of two word lists taken as sets.
func jaccard(a, b []string) float64 {
	set := map[string]int{}
	for _, w := range a {
		set[w] |= 1
	}
	for _, w := range b {
		set[w] |= 2
	}
	both := 0
	for _, in := range set {
		if in == 3 {
			both++
		}
	}
	return float64(both) / float64(len(set))
}
