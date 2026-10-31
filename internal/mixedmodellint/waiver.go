package mixedmodellint

import (
	"fmt"
	"strings"
)

// waiverScope is what a waiver is attached to: an entity, or one of its
// fields when field is set.
type waiverScope struct{ entity, field string }

// waiver is a parsed `waive` annotation: the rules or designs it covers and
// the reason it must give.
type waiver struct {
	targets []string // "7.1" covers every 7.1 design; "7.1-B" covers one
	reason  string
	pos     Position
}

// parseWaiver reads `7.1,5.1-A reason="vendor table"`. A leading "§" on a
// target is accepted, since rule_plan.md writes rules that way.
func parseWaiver(a Annotation) waiver {
	text, params := splitParams(a.Value)
	w := waiver{reason: strings.TrimSpace(params["reason"]), pos: a.Pos}
	for t := range strings.FieldsFuncSeq(text, func(r rune) bool { return r == ',' || r == ' ' }) {
		w.targets = append(w.targets, strings.TrimPrefix(t, "§"))
	}
	return w
}

func (w waiver) covers(f *Finding) bool {
	for _, t := range w.targets {
		if t == f.Rule || t == f.Design {
			return true
		}
	}
	return false
}

// applyWaivers drops the findings a waiver with a reason covers. A waiver
// on an entity covers the entity and all its fields; one on a field covers
// that field. A waiver without a reason waives nothing and is itself reported
// (rule_plan.md §3), so a bare suppression cannot slip through review.
func applyWaivers(
	findings []Finding,
	entities []*Entity,
) (kept []Finding, waived int, problems []Finding) {
	waivers, problems := collectWaivers(entities)
	for i := range findings {
		f := &findings[i]
		if f.Rule != "mml" && (coveredBy(waivers[waiverScope{f.EntityID, ""}], f) ||
			f.Field != "" && coveredBy(waivers[waiverScope{f.EntityID, f.Field}], f)) {
			waived++
			continue
		}
		kept = append(kept, *f)
	}
	return kept, waived, problems
}

// collectWaivers parses every waive annotation, returning the valid waivers
// by scope and an mml/waiver-no-reason finding for each one without a reason.
func collectWaivers(entities []*Entity) (map[waiverScope][]waiver, []Finding) {
	waivers := map[waiverScope][]waiver{}
	var problems []Finding
	add := func(e *Entity, field string, anns Annotations) {
		a, ok := anns["waive"]
		if !ok {
			return
		}
		w := parseWaiver(a)
		if w.reason != "" && len(w.targets) > 0 {
			key := waiverScope{e.ID, field}
			waivers[key] = append(waivers[key], w)
			return
		}
		problems = append(problems, Finding{
			Rule: "mml", Design: "mml/waiver-no-reason",
			Title:    "Waiver has no reason",
			Severity: SeverityWarning,
			EntityID: e.ID, Entity: e.Name, Field: field, Pos: a.Pos,
			Measured: fmt.Sprintf("waive %q has no reason=, so it waives nothing", a.Value),
			Suggestion: `Write the reason the rule does not apply, for example ` +
				`waive: 5.1 reason="vendor schema we do not control".`,
		})
	}
	for _, e := range entities {
		add(e, "", e.Annotations)
		for i := range e.Fields {
			add(e, e.Fields[i].Name, e.Fields[i].Annotations)
		}
	}
	return waivers, problems
}

func coveredBy(ws []waiver, f *Finding) bool {
	for _, w := range ws {
		if w.covers(f) {
			return true
		}
	}
	return false
}
