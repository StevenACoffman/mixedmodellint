package mixedmodellint

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
)

// Thresholds of §4.8 and §4.10: godMinAttributes is the rule's floor for a
// God entity; minSparseGroup is how many nullable fields must share a prefix
// to count as attributes of a subset of rows; siblingOverlap and
// structureOverlap are the field-name Jaccard at which two entities hold the
// same concept, with and without related names; minStructureFields keeps two
// small lookup tables from matching on structure alone.
const (
	godMinAttributes   = 30
	minSparseGroup     = 3
	siblingOverlap     = 0.6
	structureOverlap   = 0.8
	minStructureFields = 5
)

// sibling is a pair of entities §4.10 suspects hold one concept, and the
// signals that say so.
type sibling struct {
	a, b    *Entity
	named   string // the qualifier or generic noun the names differ by; "" if none
	overlap float64
}

func section4SplitDesigns() []design {
	return []design{
		{id: "4.8-A", title: "Width and sparse-subset metric", strength: Should, check: check48A},
		{
			id: "4.8-B", title: "Foreign-attribute cohesion, two-signal", strength: Should,
			heuristic: true, check: check48B,
		},
		{id: "4.9-A", title: "Undocumented junction detector", strength: Should, check: check49A},
		{
			id: "4.10-A", title: "Sibling clustering, two-signal", strength: Should,
			heuristic: true, check: check410A,
		},
	}
}

// check48A reports God entities (rule_plan.md §4.8-A): at least
// godMinAttributes attributes besides bookkeeping, of which some form
// nullable groups that share a prefix no required field has, the attributes
// of a subset of rows. Wide tables built for reading (layer=obt or feature)
// are exempt; §3.3 allows them. A sparse field with no shared prefix is not
// grouped, so the sparse count is a lower bound.
func check48A(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		layer, _ := e.Annotations.Get("layer")
		if foreignSchema(e) || e.Kind == KindView || layer == "obt" || layer == "feature" {
			continue
		}
		n := 0
		for i := range e.Fields {
			if !c.rx.metadataName.MatchString(snakeName(e.Fields[i].Name)) {
				n++
			}
		}
		groups, sparse := sparseGroups(e)
		if n < godMinAttributes || sparse == 0 {
			continue
		}
		key := cmp.Or(strings.Join(e.PrimaryKey, ", "), snakeName(e.Canonical)+"_id")
		out = append(out, issue{
			entity: e,
			measured: fmt.Sprintf("%s has %d attributes, of which %d, in nullable groups "+
				"sharing a prefix (%s), appear to apply only to some rows", e.Name, n, sparse,
				strings.Join(groups, ", ")),
			suggestion: fmt.Sprintf("Move each group to its own entity keyed by %s (for example "+
				"%s_%s), or split %s into subtype tables by a discriminator (§4.8).", key,
				snakeName(e.Canonical), strings.TrimSuffix(groups[0], "_*"), e.Name),
		})
	}
	return out
}

// sparseGroups returns the prefixes ("billing_*") shared by at least
// minSparseGroup nullable non-key fields and by no required field, and how
// many fields they hold.
func sparseGroups(e *Entity) (groups []string, fields int) {
	nullable, required := map[string]int{}, map[string]bool{}
	var order []string
	for i := range e.Fields {
		f := &e.Fields[i]
		ws := splitWords(f.Name)
		if len(ws) < 2 || isKeyOrRef(e, f.Name) {
			continue
		}
		if !f.Nullable {
			required[ws[0]] = true
			continue
		}
		if nullable[ws[0]] == 0 {
			order = append(order, ws[0])
		}
		nullable[ws[0]]++
	}
	for _, p := range order {
		if nullable[p] >= minSparseGroup && !required[p] {
			groups = append(groups, p+"_*")
			fields += nullable[p]
		}
	}
	return groups, fields
}

// check48B reports attributes of another entity kept on this one
// (rule_plan.md §4.8-B), such as orders.customer_shoe_size: the field's
// name starts with another entity's name (L), and this entity references
// that entity directly or through one hop (G), so the value depends on that
// reference rather than on this entity's key. Both signals make a warning
// (M5); the name alone is a note. Only operational entities are checked:
// event and analytical tables are denormalized on purpose. A field that
// copies a parent's field is §3.2-A's to report, and a value captured at an
// event (customer_name_at_order) is a snapshot, not a misplaced attribute.
func check48B(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		if !operational(e) || foreignSchema(e) || e.Annotations.Has("denormalized_from") {
			continue
		}
		copied := map[*Field]bool{}
		for _, cp := range c.copies(e) {
			copied[cp.field] = true
		}
		for i := range e.Fields {
			f := &e.Fields[i]
			if copied[f] {
				continue
			}
			if is, ok := c.foreignAttribute(e, f); ok {
				out = append(out, is)
			}
		}
	}
	return out
}

// foreignAttribute applies §4.8-B to one field.
func (c *catalog) foreignAttribute(e *Entity, f *Field) (issue, bool) {
	name := snakeName(f.Name)
	ws := splitWords(name)
	if len(ws) < 2 || isKeyOrRef(e, f.Name) || ws[len(ws)-1] == "id" || ws[len(ws)-1] == "key" ||
		c.rx.snapshotName.MatchString(name) || snapshot(e, f) ||
		f.Annotations.Has("denormalized_from") {
		return issue{}, false
	}
	other := c.namePrefixEntity(e, ws)
	if other == nil {
		return issue{}, false
	}
	is := issue{
		entity: e, field: f.Name, pos: f.Pos,
		measured: fmt.Sprintf("%s.%s is named for %s, so it may describe %s rather than %s",
			e.Name, f.Name, other.Name, other.Name, e.Name),
		suggestion: fmt.Sprintf("Move %s to %s and reference %s from %s, or annotate "+
			"denormalized_from=%s if it is a deliberate copy (§4.8).", f.Name, other.Name,
			other.Name, e.Name, other.Name),
	}
	if c.reaches(e, other) {
		is.level = SeverityWarning
		is.measured = fmt.Sprintf("%s.%s is named for %s, which %s references, so it "+
			"describes %s rather than %s", e.Name, f.Name, other.Name, e.Name, other.Name, e.Name)
		is.suggestion = fmt.Sprintf("Move %s to %s and read it through the reference, or "+
			"annotate denormalized_from=%s if it is a deliberate copy (§4.8).", f.Name,
			other.Name, other.Name)
	}
	return is, true
}

// namePrefixEntity returns the entity, other than e, that the longest
// leading run of words names, or nil.
func (c *catalog) namePrefixEntity(e *Entity, words []string) *Entity {
	for k := len(words) - 1; k > 0; k-- {
		if o := c.lookup(strings.Join(words[:k], "_"), e.Source); o != nil {
			if o.Canonical == e.Canonical {
				return nil
			}
			return o
		}
	}
	return nil
}

// reaches reports whether e references target directly or through one of
// its parents.
func (c *catalog) reaches(e, target *Entity) bool {
	for _, p := range c.parents(e) {
		if p == target || slices.Contains(c.parents(p), target) {
			return true
		}
	}
	return false
}

// check49A reports link tables whose definition does not say what they link
// (rule_plan.md §4.9-A): a definition that names neither side cannot be an
// explanation of the relationship (M2). A missing or thin definition is
// §1.2-A's to report and a stale one is M1's, so only a definition that
// exists is read here. removal_candidate=<ticket or date> marks a link
// nobody can explain as on its way out, which also passes.
func check49A(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		def, ok := e.Annotations.Get("definition")
		if !ok || foreignSchema(e) || e.Annotations.Has("removal_candidate") ||
			!junction(e) && !c.rx.junctionName.MatchString(e.Name) {
			continue
		}
		text, _ := splitParams(def)
		var linked, unnamed []string
		for _, p := range c.parents(e) {
			linked = append(linked, p.Name)
			if !mentions(text, p) {
				unnamed = append(unnamed, p.Name)
			}
		}
		if len(unnamed) == 0 {
			continue
		}
		out = append(out, issue{
			entity: e, pos: e.Annotations["definition"].Pos,
			measured: fmt.Sprintf("%s links %s, but its definition does not mention %s",
				e.Name, strings.Join(linked, " and "), orList(unnamed)),
			suggestion: fmt.Sprintf("Ask the domain experts what %s records and define it as "+
				"that business relationship (for example, a customer has saved a product to a "+
				"wishlist), naming what it links; if no one can say, mark it "+
				"removal_candidate=<ticket> (§4.9).", e.Name),
		})
	}
	return out
}

// mentions reports whether text names e: its canonical name, in singular or
// plural, or one of its synonyms, as whole words.
func mentions(text string, e *Entity) bool {
	words := splitWords(text)
	for i, w := range words {
		words[i] = singular(w)
	}
	names := []string{e.Canonical}
	if syn, ok := e.Annotations.Get("synonyms"); ok {
		names = append(names, splitList(syn)...)
	}
	for _, n := range names {
		if containsRun(words, strings.Split(Canonical(n), "_")) {
			return true
		}
	}
	return false
}

// containsRun reports whether run occurs in words as consecutive elements.
func containsRun(words, run []string) bool {
	for i := 0; i+len(run) <= len(words); i++ {
		if slices.Equal(words[i:i+len(run)], run) {
			return true
		}
	}
	return false
}

// check410A reports entities that look like one concept split by channel or
// by a generic noun (rule_plan.md §4.10-A): chat_tickets and email_tickets,
// or customer_profile and customer_account (L), whose fields mostly match
// (S). Both signals make a warning (M5). Names alone, or a near-identical
// structure with unrelated names, make a note. A pair that shares an
// identifier, one referencing the other or both keyed by the same parent,
// is a subtype split and passes, as does one that declares split_reason.
func check410A(c *catalog) []issue {
	var out []issue
	for i, a := range c.entities {
		for _, b := range c.entities[i+1:] {
			if s, ok := c.siblingOf(a, b); ok {
				out = append(out, siblingIssue(&s))
			}
		}
	}
	return out
}

// siblingOf returns the §4.10 signals for a and b, and false when they are
// not suspect.
func (c *catalog) siblingOf(a, b *Entity) (sibling, bool) {
	if c.splitAccountedFor(a, b) {
		return sibling{}, false
	}
	fa, fb := c.conceptFields(a), c.conceptFields(b)
	s := sibling{a: a, b: b, named: c.nameSignal(a, b)}
	if len(fa) > 0 && len(fb) > 0 {
		s.overlap = jaccard(fa, fb)
	}
	structural := s.overlap >= structureOverlap && min(len(fa), len(fb)) >= minStructureFields
	return s, s.named != "" || structural
}

// splitAccountedFor reports whether a and b need no §4.10 check: they are
// one entity in two sources or in different source kinds, someone else's
// data, a declared split, or subtypes sharing an identifier.
func (c *catalog) splitAccountedFor(a, b *Entity) bool {
	return a.Source != b.Source || a.Canonical == b.Canonical || foreignSchema(a) ||
		foreignSchema(b) || a.Annotations.Has("split_reason") ||
		b.Annotations.Has("split_reason") || c.sharedIdentifier(a, b)
}

// nameSignal returns how a's and b's names say they are one concept: the
// qualifiers before a shared head noun ("chat or email") or the generic
// nouns after a shared stem ("profile or account"), or "".
func (c *catalog) nameSignal(a, b *Entity) string {
	wa, wb := strings.Split(a.Canonical, "_"), strings.Split(b.Canonical, "_")
	if len(wa) != len(wb) || len(wa) < 2 {
		return ""
	}
	last := len(wa) - 1
	switch {
	case wa[last] == wb[last] && slices.Equal(wa[1:], wb[1:]):
		return wa[0] + " or " + wb[0]
	case slices.Equal(wa[:last], wb[:last]) && c.rx.genericNoun[wa[last]] &&
		c.rx.genericNoun[wb[last]]:
		return wa[last] + " or " + wb[last]
	default:
		return ""
	}
}

// conceptFields returns e's field names that describe the concept, leaving
// out keys, references, bookkeeping and generic lookup columns.
func (c *catalog) conceptFields(e *Entity) []string {
	var out []string
	for i := range e.Fields {
		name := snakeName(e.Fields[i].Name)
		if isKeyOrRef(e, e.Fields[i].Name) || c.rx.metadataName.MatchString(name) ||
			c.rx.genericField[name] {
			continue
		}
		out = append(out, name)
	}
	return out
}

// sharedIdentifier reports whether a and b share an identifier: one
// references the other, or both reference one entity through their primary
// keys, as subtype tables of one supertype do.
func (c *catalog) sharedIdentifier(a, b *Entity) bool {
	if c.refersTo(a, b) || c.refersTo(b, a) {
		return true
	}
	for _, r := range a.Refs {
		t := c.lookup(r.Target, a.Source)
		if t != nil && sameColumns(r.Fields, a.PrimaryKey) && slices.ContainsFunc(b.Refs,
			func(rb Ref) bool {
				return sameColumns(rb.Fields, b.PrimaryKey) && c.lookup(rb.Target, b.Source) == t
			}) {
			return true
		}
	}
	return false
}

// siblingIssue reports a suspect pair at warning when names and fields
// agree, and at the design's note otherwise.
func siblingIssue(s *sibling) issue {
	is := issue{
		entity: s.a,
		suggestion: fmt.Sprintf("Merge %s and %s into one entity with a column that says "+
			"which kind each row is, or, if the split is deliberate (performance or access "+
			"control), key both by one shared ID and declare split_reason (§4.10).",
			s.a.Name, s.b.Name),
	}
	share := fmt.Sprintf("%.0f%% of their fields match", s.overlap*100)
	switch {
	case s.named != "" && s.overlap >= siblingOverlap:
		is.level = SeverityWarning
		is.measured = fmt.Sprintf("%s and %s differ only by %s in their names, and %s",
			s.a.Name, s.b.Name, s.named, share)
	case s.named != "":
		is.measured = fmt.Sprintf("%s and %s differ only by %s in their names (%s)",
			s.a.Name, s.b.Name, s.named, share)
	default:
		is.measured = fmt.Sprintf("%s and %s have unrelated names, but %s", s.a.Name,
			s.b.Name, share)
	}
	return is
}
