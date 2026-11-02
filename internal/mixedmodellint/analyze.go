package mixedmodellint

import (
	"fmt"
	"slices"
	"strings"
)

// Input is everything one lint run reads, already loaded into values by the
// adapters.
type Input struct {
	Sources   []Source
	Sidecar   Sidecar
	Baseline  Baseline
	Artifacts Artifacts
	// Lock is the committed schema lock, or nil when the repository has
	// none.
	Lock *Lock
	// Base is the model at the change's base revision (check --base), or
	// nil. The designs that compare a change with the previous model read
	// Base when it is set and the lock otherwise; with neither they do not
	// run.
	Base *Input
}

// Sidecar is the mml.yaml catalog: annotations for entities whose source
// cannot carry them inline (a vendor schema, a generated struct). Entities is
// keyed by entity ID, name, or canonical name, in that order of precedence.
type Sidecar struct {
	File     string
	Entities map[string]SidecarEntity
}

// SidecarEntity holds the sidecar's annotations for one entity and its fields.
type SidecarEntity struct {
	Annotations Annotations
	Fields      map[string]Annotations
}

// Report is the result of a lint run.
type Report struct {
	Findings  []Finding
	Entities  int
	Queries   int
	Waived    int
	Baselined int
}

// HasErrors reports whether any finding is an error, which fails CI.
func (r Report) HasErrors() bool {
	return slices.ContainsFunc(r.Findings, func(f Finding) bool {
		return f.Severity == SeverityError
	})
}

// Analyze links the entities of every source, merges the sidecar, runs every
// implemented design, applies waivers and the baseline, and returns the
// findings in a deterministic order.
//
// Requires: nothing beyond well-formed values; empty input is valid.
// Ensures:  the same Input always yields the same Report; Report.Findings is
// sorted by position; in is not modified; no finding a waiver with a reason covers, and no finding
// whose fingerprint is in the baseline, is returned.
func Analyze(in *Input) Report {
	entities, queries, findings := prepare(in)
	c := newCatalog(entities, queries, newLexicon())
	c.artifacts, c.sidecar = in.Artifacts, in.Sidecar
	if in.Lock != nil {
		c.previous = in.Lock.restore()
		findings = append(findings, lockStaleFindings(diffLock(c.previous, entities))...)
	}
	if in.Base != nil {
		c.previous, _, _ = prepare(in.Base)
	}
	findings = append(findings, run(c, allDesigns())...)

	findings, waived, waiverFindings := applyWaivers(findings, entities)
	findings = append(findings, waiverFindings...)
	findings, baselined := in.Baseline.filter(findings)
	sortFindings(findings)
	return Report{
		Findings:  findings,
		Entities:  len(entities),
		Queries:   len(queries),
		Waived:    waived,
		Baselined: baselined,
	}
}

// prepare builds the model every design reads, and that a lock records: the
// sources' entities, copied, with the sidecar merged, ref= declarations
// added and canonical names set; the query corpus; and the findings made
// while reading them.
func prepare(in *Input) (entities []*Entity, queries []Query, findings []Finding) {
	for i := range in.Sources {
		src := &in.Sources[i]
		// Copies, so linking and the sidecar merge never write through to the
		// caller's Input (summary_rules §5: do not mutate a parameter).
		for j := range src.Entities {
			e := src.Entities[j]
			e.Fields, e.Refs = slices.Clone(e.Fields), slices.Clone(e.Refs)
			entities = append(entities, &e)
		}
		queries = append(queries, src.Queries...)
		findings = append(findings, problemFindings(src.Problems)...)
	}

	// Annotations first: linking reads `entity`, and every design reads the
	// merged result.
	findings = append(findings, mergeSidecar(entities, in.Sidecar)...)
	findings = append(findings, unknownKeyFindings(entities)...)
	for _, e := range entities {
		declareRefs(e)
		e.Canonical = Canonical(e.Name)
		if v, ok := e.Annotations.Get("entity"); ok {
			e.Canonical = Canonical(v)
		}
	}
	return entities, queries, findings
}

func problemFindings(problems []Problem) []Finding {
	out := make([]Finding, 0, len(problems))
	for _, p := range problems {
		out = append(out, Finding{
			Rule:     "mml",
			Design:   "mml/parse-error",
			Title:    "Source could not be read",
			Severity: SeverityError,
			Pos:      p.Pos,
			Measured: p.Message,
			Suggestion: "Fix the syntax error, or exclude the file from the run. " +
				"Entities in this statement were not checked.",
		})
	}
	return out
}

// declareRefs adds a reference for each field annotated ref=<entity> that no
// declared reference covers yet: the declaration a source without foreign
// keys makes (JSON x-mml-ref, a Go mml:"ref=..." tag, a SQL @ref comment,
// mml.yaml).
func declareRefs(e *Entity) {
	for i := range e.Fields {
		f := &e.Fields[i]
		target, ok := f.Annotations.Get("ref")
		if !ok {
			continue
		}
		declared := Ref{Fields: []string{f.Name}, Target: target, Pos: f.Annotations["ref"].Pos}
		j := slices.IndexFunc(e.Refs, func(r Ref) bool { return containsFold(r.Fields, f.Name) })
		switch {
		case j < 0:
			e.Refs = append(e.Refs, declared)
		case e.Refs[j].Inferred:
			e.Refs[j] = declared // a declaration replaces a guess from the name
		}
	}
}

// lockStaleFindings reports each entity that differs from mml.lock.json, so
// a structural change cannot land without the reviewed lock diff that
// records it (M3).
func lockStaleFindings(changes []lockChange) []Finding {
	out := make([]Finding, 0, len(changes))
	for i := range changes {
		ch := &changes[i]
		f := Finding{
			Rule: "mml", Design: "mml/lock-stale",
			Title:    "Model differs from mml.lock.json",
			Severity: SeverityWarning,
			Suggestion: "Run mixedmodellint lock and commit mml.lock.json with this change, " +
				"so reviewers see the structural diff.",
		}
		switch {
		case ch.locked == nil:
			f.EntityID, f.Entity, f.Pos = ch.current.ID, ch.current.Name, ch.current.Pos
			f.Measured = fmt.Sprintf("%s is not in mml.lock.json", ch.current.Name)
		case ch.current == nil:
			f.EntityID, f.Entity = ch.locked.ID, ch.locked.Name
			f.Pos = Position{File: "mml.lock.json"}
			f.Measured = fmt.Sprintf("%s is in mml.lock.json but no longer in the sources",
				ch.locked.Name)
		default:
			f.EntityID, f.Entity, f.Pos = ch.current.ID, ch.current.Name, ch.current.Pos
			f.Measured = fmt.Sprintf("%s changed since mml.lock.json: %s", ch.current.Name,
				ch.describe())
		}
		out = append(out, f)
	}
	return out
}

// mergeSidecar copies sidecar annotations onto entities and fields. An inline
// annotation wins over the sidecar, and a disagreement between them is an
// mml/annotation-conflict warning, since one of the two is stale.
func mergeSidecar(entities []*Entity, sc Sidecar) []Finding {
	if len(sc.Entities) == 0 {
		return nil
	}
	var out []Finding
	for _, e := range entities {
		se, ok := sidecarFor(sc, e)
		if !ok {
			continue
		}
		e.Annotations, out = mergeInto(e, "", e.Annotations, se.Annotations, out)
		for i := range e.Fields {
			f := &e.Fields[i]
			for name, anns := range se.Fields {
				if strings.EqualFold(name, f.Name) {
					f.Annotations, out = mergeInto(e, f.Name, f.Annotations, anns, out)
				}
			}
		}
	}
	return out
}

func sidecarFor(sc Sidecar, e *Entity) (SidecarEntity, bool) {
	for _, key := range []string{e.ID, e.Name, Canonical(e.Name)} {
		if se, ok := sc.Entities[key]; ok {
			return se, true
		}
	}
	return SidecarEntity{}, false
}

func mergeInto(
	e *Entity, field string, inline, side Annotations, out []Finding,
) (Annotations, []Finding) {
	merged := Annotations{}
	for k, v := range side {
		merged[k] = v
	}
	keys := make([]string, 0, len(inline))
	for k := range inline {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		v := inline[k]
		if s, ok := side[k]; ok && strings.TrimSpace(s.Value) != strings.TrimSpace(v.Value) {
			out = append(out, Finding{
				Rule: "mml", Design: "mml/annotation-conflict",
				Title:    "Inline annotation disagrees with mml.yaml",
				Severity: SeverityWarning,
				EntityID: e.ID, Entity: e.Name, Field: field, Pos: v.Pos,
				Measured: fmt.Sprintf("%s is %q inline but %q in %s", k, v.Value, s.Value,
					s.Pos.File),
				Suggestion: fmt.Sprintf("Keep one of the two declarations of %s; the "+
					"inline value is the one in effect.", k),
			})
		}
		merged[k] = v
	}
	return merged, out
}

func unknownKeyFindings(entities []*Entity) []Finding {
	var out []Finding
	report := func(e *Entity, field string, anns Annotations) {
		for k, v := range anns {
			if knownKey(k) {
				continue
			}
			out = append(out, Finding{
				Rule: "mml", Design: "mml/unknown-key",
				Title:    "Annotation key is not in the vocabulary",
				Severity: SeverityNote,
				EntityID: e.ID, Entity: e.Name, Field: field, Pos: v.Pos,
				Measured: fmt.Sprintf("annotation key %q is not a known mml key", k),
				Suggestion: "Check the spelling against rule_plan.md §2; " +
					"no rule reads this key.",
			})
		}
	}
	for _, e := range entities {
		report(e, "", e.Annotations)
		for i := range e.Fields {
			f := &e.Fields[i]
			report(e, f.Name, f.Annotations)
		}
	}
	return out
}
