package mixedmodellint

import (
	"fmt"
	"slices"
	"strings"
)

// Shares of a field's values in one standard at which §2.7 acts: closeOverlap
// is where 2.7-B reports the field as that standard with local edits, and
// partialOverlap is where it is a second signal for 2.7-A.
const (
	closeOverlap   = 0.8
	partialOverlap = 0.5
	minEnumValues  = 3
)

// overlap is how closely a field's values follow one value set: the share
// that are its codes or known aliases of them, and the aliases used.
type overlap struct {
	set        *valueSet
	share      float64
	deviations []deviation
}

// deviation is a local value used where the standard has another code.
type deviation struct {
	local, standard string
}

func section2VocabDesigns() []design {
	return []design{
		{
			id: "2.7-A", title: "Regulated-domain code-list detector", strength: Should,
			heuristic: true, check: check27A,
		},
		{
			id: "2.7-B", title: "Local enum vs standard value-set overlap", strength: Should,
			check: check27B,
		},
	}
}

// check27B reports enums that are a standard code list kept by hand: most
// values are one standard's codes, yet the field does not say so, or uses
// variants (UK where ISO 3166 assigns GB). standard=local with a reason
// records a decision to keep the local list.
func check27B(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		if foreignSchema(e) {
			continue
		}
		for i := range e.Fields {
			if is, ok := c.handKeptStandard(e, &e.Fields[i]); ok {
				out = append(out, is)
			}
		}
	}
	return out
}

// handKeptStandard applies §2.7-B to one field.
func (c *catalog) handKeptStandard(e *Entity, f *Field) (issue, bool) {
	values := enumValues(e, f)
	if len(values) < minEnumValues {
		return issue{}, false
	}
	o := c.bestOverlap(f.Name, values)
	declared, isDeclared := f.Annotations.Get("standard")
	if o.share < closeOverlap || strings.HasPrefix(declared, "local") ||
		isDeclared && len(o.deviations) == 0 {
		return issue{}, false
	}
	measured := fmt.Sprintf("%.0f%% of the values %s.%s allows are %s codes", o.share*100,
		e.Name, f.Name, o.set.standard)
	fix := ""
	if len(o.deviations) > 0 {
		used, replace := make([]string, 0, len(o.deviations)), make([]string, 0, len(o.deviations))
		for _, d := range o.deviations {
			used = append(used, d.local+" for "+d.standard)
			replace = append(replace, d.local+" with "+d.standard)
		}
		measured += ", but it uses " + strings.Join(used, ", ")
		fix = ", replacing " + strings.Join(replace, ", ") + ","
	}
	if !isDeclared {
		measured += ", and it does not declare the standard"
	}
	return issue{
		entity: e, field: f.Name, pos: f.Pos, measured: measured,
		suggestion: fmt.Sprintf("Adopt the %s codes%s and annotate standard=%s, or "+
			"record standard=local reason=\"...\" if the local list is deliberate (§2.7).",
			o.set.standard, fix, o.set.standard),
	}, true
}

// bestOverlap returns the value set values follow most closely. When two
// follow equally (FR, DE and IT are countries and languages both), the one
// the field's name hints at wins, then the first listed.
//
// Requires: len(values) > 0.
func (c *catalog) bestOverlap(fieldName string, values []string) overlap {
	var best overlap
	for i := range c.rx.valueSets {
		o := overlapWith(&c.rx.valueSets[i], values)
		switch {
		case best.set == nil, o.share > best.share:
			best = o
		case o.share == best.share && o.set.nameHint.MatchString(fieldName) &&
			!best.set.nameHint.MatchString(fieldName):
			best = o
		}
	}
	return best
}

// overlapWith measures values against vs, ignoring case, and lists the
// aliases used, sorted so the message does not depend on the order the
// values were declared in.
func overlapWith(vs *valueSet, values []string) overlap {
	o := overlap{set: vs}
	hits := 0
	for _, v := range values {
		code := strings.ToUpper(strings.TrimSpace(v))
		if vs.codes[code] {
			hits++
		} else if std, ok := vs.aliases[code]; ok {
			hits++
			o.deviations = append(o.deviations, deviation{local: v, standard: std})
		}
	}
	slices.SortFunc(
		o.deviations,
		func(a, b deviation) int { return strings.Compare(a.local, b.local) },
	)
	o.share = float64(hits) / float64(len(values))
	return o
}

// check27A reports fields that hold a concept with a standard vocabulary,
// such as a diagnosis or a country, without saying which standard they
// follow. A name match alone is a note; it reaches warning when a second,
// independent signal agrees (M5): the field's values partly match the
// concept's bundled value set, or its description names the standard. A
// close value match is 2.7-B's to report, so it is left to that design.
func check27A(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		if foreignSchema(e) || e.Annotations.Has("standard") {
			continue
		}
		for i := range e.Fields {
			if is, ok := c.unstatedStandard(e, &e.Fields[i]); ok {
				out = append(out, is)
			}
		}
	}
	return out
}

// unstatedStandard applies §2.7-A to one field: a field referencing a table
// that declares its standard follows that standard.
func (c *catalog) unstatedStandard(e *Entity, f *Field) (issue, bool) {
	rc, ok := c.regulatedConcept(f)
	if !ok || f.Type == TypeBool || f.Annotations.Has("standard") {
		return issue{}, false
	}
	if t := c.referenced(e, f); t != nil && t.Annotations.Has("standard") {
		return issue{}, false
	}
	share := c.conceptOverlap(rc, enumValues(e, f))
	if share >= closeOverlap {
		return issue{}, false
	}
	is := issue{
		entity: e, field: f.Name, pos: f.Pos,
		measured: fmt.Sprintf("%s.%s looks like a %s code but does not say which standard "+
			"it follows", e.Name, f.Name, rc.concept),
		suggestion: fmt.Sprintf("Use %s rather than a local list and name it in the "+
			"field's standard annotation (standard=%s), or record standard=local "+
			"reason=\"...\" to keep the local list deliberately (§2.7).", rc.standard,
			strings.Split(rc.standard, " or ")[0]),
	}
	switch {
	case share >= partialOverlap:
		is.level = SeverityWarning
		is.measured += fmt.Sprintf("; %.0f%% of its values are already %s codes", share*100,
			rc.values)
	case rc.doc.MatchString(f.Doc):
		is.level = SeverityWarning
		is.measured += "; its description names the standard"
	}
	return is, true
}

// regulatedConcept returns the first concept f's name matches.
func (c *catalog) regulatedConcept(f *Field) (*regulatedConcept, bool) {
	name := snakeName(f.Name)
	for i := range c.rx.regulated {
		if c.rx.regulated[i].name.MatchString(name) {
			return &c.rx.regulated[i], true
		}
	}
	return nil, false
}

// conceptOverlap returns the share of values in rc's bundled value set, or 0
// when rc has none or there are too few values to tell.
func (c *catalog) conceptOverlap(rc *regulatedConcept, values []string) float64 {
	if rc.values == "" || len(values) < minEnumValues {
		return 0
	}
	for i := range c.rx.valueSets {
		if vs := &c.rx.valueSets[i]; vs.standard == rc.values {
			return overlapWith(vs, values).share
		}
	}
	return 0
}
