package mixedmodellint

import (
	"cmp"
	"slices"
)

// Stub is one annotation the annotate command proposes: an entity (by
// canonical name, as mml.yaml keys it), optionally one of its fields, a key
// and a value. The value is inferred from the structure where it can be
// (M2) and TODO where only a person can say, which no design accepts, so a
// stub left unfilled is still reported.
type Stub struct {
	Entity string
	Field  string
	Key    string
	Value  string
}

// Stubs proposes the annotations the declaration designs ask for and the
// model does not have yet: each entity's grain (inferred from its key, with
// the fingerprint it was inferred from), definition, owner and escalation;
// each nullable field's null meaning and each nullable reference's
// cardinality; and each measure's class (proposed from its name).
//
// Ensures: stubs are sorted by entity, field and key; an annotation already
// present, inline or in mml.yaml, is never proposed; in is not modified.
func Stubs(in *Input) []Stub {
	entities, _, _ := prepare(in)
	var out []Stub
	for _, e := range entities {
		out = append(out, entityStubs(e)...)
		for i := range e.Fields {
			out = append(out, fieldStubs(e, &e.Fields[i])...)
		}
	}
	slices.SortFunc(out, func(a, b Stub) int {
		return cmp.Or(cmp.Compare(a.Entity, b.Entity), cmp.Compare(a.Field, b.Field),
			cmp.Compare(a.Key, b.Key))
	})
	return slices.CompactFunc(out, func(a, b Stub) bool { return a == b })
}

func entityStubs(e *Entity) []Stub {
	var out []Stub
	add := func(key, value string) {
		if !e.Annotations.Has(key) {
			out = append(out, Stub{Entity: e.Canonical, Key: key, Value: value})
		}
	}
	if g := inferGrain(e); g != "" {
		add("grain", g+" fp="+keyFingerprint(e))
	} else {
		add("grain", "TODO")
	}
	if governed(e) {
		add("definition", "TODO")
		add("owner", "TODO")
		add("escalation", "TODO")
	}
	return out
}

func fieldStubs(e *Entity, f *Field) []Stub {
	var out []Stub
	add := func(key, value string) {
		if !f.Annotations.Has(key) {
			out = append(out, Stub{Entity: e.Canonical, Field: f.Name, Key: key, Value: value})
		}
	}
	if f.Nullable && !containsFold(e.PrimaryKey, f.Name) {
		add("null", "TODO")
		if slices.ContainsFunc(e.Refs, func(r Ref) bool {
			return !r.Inferred && containsFold(r.Fields, f.Name)
		}) {
			add("card", "TODO")
		}
	}
	if measureScoped(e) && measureCandidate(e, f) {
		add("measure", measureGuess(f.Name)+overHint(measureGuess(f.Name)))
	}
	return out
}
