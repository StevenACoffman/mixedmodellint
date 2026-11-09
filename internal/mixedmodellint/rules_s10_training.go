package mixedmodellint

import (
	"fmt"
	"strings"
)

func section10TrainingDesigns() []design {
	return []design{{
		id: "10.5-A", title: "Training-set contract", strength: Must, check: check105A,
	}}
}

// check105A reports a training set whose declarations cannot keep future
// data out of its rows (rule_plan.md §10.5-A): it must name its label and its
// observation time, the observation time must be a NOT NULL time in a key,
// and every feature table it references must record when each value held.
// The grain statement and its agreement with the keys are §7.1-A's and
// §7.1-B's, which run on every entity. This proves the declarations agree
// with the shape, not that the build query honors them (§10.5-B).
func check105A(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		why, ok := trainingReason(e)
		if !ok {
			continue
		}
		gaps := append(declaredGaps(e), c.featureGaps(e)...)
		if len(gaps) == 0 {
			continue
		}
		out = append(out, issue{
			entity: e,
			measured: fmt.Sprintf("%s is a training set (%s) but %s", e.Name, why,
				strings.Join(gaps, "; ")),
			suggestion: "Declare label=<column> and observation_time=<column> on it, make " +
				"the observation time a NOT NULL timestamp in the key (one row per entity per " +
				"observation), and give each feature table an as_of time so its values can be " +
				"joined as they were at that time (§10.5).",
		})
	}
	return out
}

// trainingReason says why e is taken for a training set: layer=training, or
// a name with the word training, a leading train or a last word labels or
// examples. A different declared layer overrides the name, so a
// shipping_labels table can say what it is.
func trainingReason(e *Entity) (string, bool) {
	if outsideConventions(e) || (!storedRecord(e) && e.Kind != KindView) {
		return "", false
	}
	if layer, ok := e.Annotations.Get("layer"); ok {
		return "layer=training", layer == "training"
	}
	return trainingName(splitWords(e.Name))
}

// trainingName reports a name with the word training, a leading train
// (train_churn), or a last word labels or examples (churn_labels).
func trainingName(words []string) (string, bool) {
	for i, w := range words {
		last := i > 0 && i == len(words)-1
		switch {
		case w == "training", i == 0 && w == "train" && len(words) > 1,
			last && (singular(w) == "label" || singular(w) == "example"):
			return "its name says " + w, true
		}
	}
	return "", false
}

// declaredGaps lists what is wrong with e's label and observation_time
// declarations.
func declaredGaps(e *Entity) []string {
	var gaps []string
	if label, ok := e.Annotations.Get("label"); !ok {
		gaps = append(gaps, "it declares no label")
	} else if _, ok := e.Field(label); !ok {
		gaps = append(gaps, fmt.Sprintf("its label %s is not a field", label))
	}
	obs, ok := e.Annotations.Get("observation_time")
	if !ok {
		return append(gaps, "it declares no observation_time")
	}
	f, ok := e.Field(obs)
	switch {
	case !ok:
		gaps = append(gaps, fmt.Sprintf("its observation_time %s is not a field", obs))
	case !isTemporal(f.Type):
		gaps = append(gaps, fmt.Sprintf("its observation_time %s is a %s, not a time", obs,
			f.Type))
	case f.Nullable:
		gaps = append(gaps, fmt.Sprintf("its observation_time %s may be NULL", obs))
	case !inSomeKey(e, f.Name):
		gaps = append(gaps, fmt.Sprintf("no key includes its observation_time %s", obs))
	}
	return gaps
}

// inSomeKey reports whether the primary key or a UNIQUE key of e includes
// the field name.
func inSomeKey(e *Entity, name string) bool {
	for _, k := range e.Keys() {
		if containsFold(k, name) {
			return true
		}
	}
	return false
}

// featureGaps names the layer=feature entities e references that record no
// as-of time, so a join cannot pick the value that held at observation time.
func (c *catalog) featureGaps(e *Entity) []string {
	var gaps []string
	seen := map[*Entity]bool{}
	for _, r := range e.Refs {
		t := c.lookup(r.Target, e.Source)
		if t == nil || seen[t] {
			continue
		}
		seen[t] = true
		if layer, _ := t.Annotations.Get("layer"); layer == "feature" && !hasAsOf(t) {
			gaps = append(gaps, fmt.Sprintf("its feature table %s has no as_of or "+
				"valid_from time", t.Name))
		}
	}
	return gaps
}

// hasAsOf reports whether e has a time saying when each row's values held: a
// field declared time=valid_from or time=event, or named as_of, as_of_* or
// valid_from.
func hasAsOf(e *Entity) bool {
	for i := range e.Fields {
		f := &e.Fields[i]
		if !isTemporal(f.Type) {
			continue
		}
		role, _ := f.Annotations.Get("time")
		name := strings.ToLower(f.Name)
		if role == "valid_from" || role == "event" || name == "as_of" ||
			strings.HasPrefix(name, "as_of_") || name == "valid_from" {
			return true
		}
	}
	return false
}
