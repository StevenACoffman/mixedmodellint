package mixedmodellint

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// maxEmbeddedItems is the bound above which §6.5-B treats an embedded list
// as unbounded: a document that holds more is a collection in disguise.
const maxEmbeddedItems = 100

// lineItem is an entity recording one part of a transaction: the header it
// belongs to and the master entities it names.
type lineItem struct {
	header  *Entity
	masters []*Entity
}

func section6CaptureDesigns() []design {
	return []design{
		{
			id: "6.5-B", title: "Unbounded-growth embedding", strength: Should, heuristic: true,
			check: check65B,
		},
		{
			id: "6.6-A", title: "Line item missing its price snapshot", strength: Should,
			heuristic: true, check: check66A,
		},
		{
			id: "6.7-A", title: "Inferred-link field completeness", strength: Should,
			heuristic: true, check: check67A,
		},
	}
}

// check65B reports an embedded list named like a stream that grows, such as
// order_history or events, with no bound of at most maxEmbeddedItems
// (rule_plan.md §6.5-B): the document grows with every event until it hits
// the store's size limit. A bound is max-items (maxItems in JSON Schema).
// Until the Phase 2 code corpus can show an append, the name is the only
// signal, so findings stay notes. A list of references is §6.1-A's.
func check65B(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		if outsideConventions(e) {
			continue
		}
		for i := range e.Fields {
			f := &e.Fields[i]
			if f.Type != TypeArray || !c.rx.growingList.MatchString(f.Name) || bounded(f) {
				continue
			}
			if _, refs := c.referenceList(e, f); refs {
				continue
			}
			out = append(out, issue{
				entity: e, field: f.Name, pos: f.Pos,
				measured: fmt.Sprintf("%s.%s embeds a list named like one that only grows, "+
					"with no bound of at most %d items", e.Name, f.Name, maxEmbeddedItems),
				suggestion: fmt.Sprintf("Move the items to their own collection keyed by "+
					"%s_id, or declare max-items (JSON Schema: maxItems) of at most %d if the "+
					"list is truly bounded (§6.5).", snakeName(e.Canonical), maxEmbeddedItems),
			})
		}
	}
	return out
}

// bounded reports whether f declares a bound of at most maxEmbeddedItems.
func bounded(f *Field) bool {
	v, ok := f.Annotations.Get("max-items")
	n, err := strconv.Atoi(strings.TrimSpace(v))
	return ok && err == nil && n <= maxEmbeddedItems
}

// check66A reports a line item that does not record the price it was sold
// at (rule_plan.md §6.6-A): its master has a value that changes over time,
// such as products.unit_price, and the line has no field for it, so
// historical totals change whenever the price does. A price named as one is
// a note; when the master also has validity columns, its values change by
// design and it is a warning (M5).
func check66A(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		li, ok := c.lineItemOf(e)
		if !ok || outsideConventions(e) {
			continue
		}
		for _, m := range li.masters {
			out = append(out, c.missingSnapshots(e, m)...)
		}
	}
	return out
}

// lineItemOf returns e as a line item: a key containing a reference to its
// header, and references to other entities, its masters. An entity with no
// key is judged by its name, its first reference taken as the header.
func (c *catalog) lineItemOf(e *Entity) (lineItem, bool) {
	var li lineItem
	keys := e.Keys()
	for i := range e.Refs {
		r := &e.Refs[i]
		t := c.lookup(r.Target, e.Source)
		switch {
		case t == nil || t == e:
		case li.header == nil && (keyContains(keys, r.Fields) ||
			len(keys) == 0 && c.rx.lineName.MatchString(e.Name)):
			li.header = t
		default:
			li.masters = append(li.masters, t)
		}
	}
	return li, li.header != nil && len(li.masters) > 0
}

// keyContains reports whether some key holds every column of cols.
func keyContains(keys [][]string, cols []string) bool {
	return len(cols) > 0 && slices.ContainsFunc(keys, func(k []string) bool {
		return !slices.ContainsFunc(cols, func(col string) bool { return !containsFold(k, col) })
	})
}

// missingSnapshots reports each volatile numeric field of master m that line
// item e keeps no copy of.
func (c *catalog) missingSnapshots(e, m *Entity) []issue {
	var out []issue
	validity := anyField(m, func(f *Field) bool {
		return c.rx.validity.MatchString(snakeName(f.Name))
	})
	for i := range m.Fields {
		f := &m.Fields[i]
		word := strings.ToLower(c.rx.volatile.FindString(f.Name))
		if word == "" || !isNumeric(f.Type) || anyField(e, func(lf *Field) bool {
			return strings.Contains(strings.ToLower(lf.Name), word)
		}) {
			continue
		}
		is := issue{
			entity: e, field: f.Name, pos: e.Pos,
			measured: fmt.Sprintf("%s references %s but keeps no copy of %s.%s from when the "+
				"transaction happened", e.Name, m.Name, m.Name, f.Name),
			suggestion: fmt.Sprintf("Add %s.%s %s NOT NULL, copied from %s at insert, so "+
				"historical totals do not change when %s does (§6.6).", e.Name, f.Name,
				strings.ToUpper(cmp.Or(f.RawType, "numeric")), m.Name, f.Name),
		}
		if validity {
			is.level = SeverityWarning
			is.measured += fmt.Sprintf("; %s has validity columns, so %s changes by design",
				m.Name, f.Name)
		}
		out = append(out, is)
	}
	return out
}

// check67A reports an entity of inferred links, such as candidate matches
// or recommendations, that does not record how sure each link is, how it
// was made, and whether a person has reviewed it (rule_plan.md §6.7-A). It
// is found by its name, or by a score field beside two references. An
// identity map from source IDs is a deterministic mapping, not an
// inference, and is left to §4.2.
func check67A(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		if outsideConventions(e) || mappingShaped(e) || !c.inferredLinks(e) {
			continue
		}
		if missing := c.inferenceGaps(e); len(missing) > 0 {
			out = append(out, issue{
				entity: e,
				measured: fmt.Sprintf("%s records inferred links but lacks %s", e.Name,
					andList(missing)),
				suggestion: "Add confidence NUMERIC NOT NULL, match_method TEXT NOT NULL and " +
					"review_status CHECK (review_status IN ('auto_applied', 'pending_review', " +
					"'rejected', 'confirmed')) (§6.7).",
			})
		}
	}
	return out
}

// inferredLinks reports whether e records links an algorithm inferred.
func (c *catalog) inferredLinks(e *Entity) bool {
	if c.rx.inferredName.MatchString(e.Name) {
		return true
	}
	if len(e.Refs) < 2 {
		return false
	}
	for i := range e.Fields {
		if c.rx.scoreName.MatchString(e.Fields[i].Name) {
			return true
		}
	}
	return false
}

// inferenceGaps lists what e lacks of a confidence, a method and a review
// decision that can say a person must look.
func (c *catalog) inferenceGaps(e *Entity) []string {
	needs := []struct {
		what string
		met  func(f *Field) bool
	}{
		{"a required numeric confidence", func(f *Field) bool {
			return c.rx.confidence.MatchString(snakeName(f.Name)) && isNumeric(f.Type) &&
				!f.Nullable
		}},
		{"a match method or model version", func(f *Field) bool {
			return c.rx.method.MatchString(snakeName(f.Name))
		}},
		{"a review status with a pending-review state", func(f *Field) bool {
			return c.rx.decision.MatchString(snakeName(f.Name)) &&
				slices.ContainsFunc(enumValues(e, f), c.rx.reviewState.MatchString)
		}},
	}
	var missing []string
	for _, n := range needs {
		if !anyField(e, n.met) {
			missing = append(missing, n.what)
		}
	}
	return missing
}

// anyField reports whether some field of e satisfies met.
func anyField(e *Entity, met func(f *Field) bool) bool {
	for i := range e.Fields {
		if met(&e.Fields[i]) {
			return true
		}
	}
	return false
}
