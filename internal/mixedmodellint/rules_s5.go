package mixedmodellint

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// siblingGroup is one family of numbered columns: phone1, phone2, phone3.
type siblingGroup struct {
	stem    string
	suffix  string
	numbers []int
	first   *Field
}

func section5Designs() []design {
	return []design{
		{
			id:       "5.1-A",
			title:    "Reserved-word and bare-type lexicon",
			strength: Must,
			check:    check51A,
		},
		{id: "5.5-A", title: "Numbered-sibling detector", strength: Should, check: check55A},
	}
}

// thirdParty reports whether e's schema belongs to someone else, which
// exempts it from naming rules it cannot follow (§3.5).
func thirdParty(e *Entity) bool {
	s, _ := e.Annotations.Get("source")
	return s == "third_party"
}

func check51A(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		if thirdParty(e) {
			continue
		}
		categorical := countCategorical(e)
		for i := range e.Fields {
			f := &e.Fields[i]
			if is, ok := nameIssue(e, f, categorical); ok {
				out = append(out, is)
			}
		}
	}
	return out
}

// nameIssue grades one field name by §5.1-A's lists: reserved words and
// ambiguous type words are errors, generic names warnings, and a weakly
// ambiguous "type" or "kind" a note, raised to a warning when the entity has
// two or more categorical fields that a bare "type" cannot tell apart.
func nameIssue(e *Entity, f *Field, categorical int) (issue, bool) {
	name := strings.ToLower(f.Name)
	base := issue{entity: e, field: f.Name, pos: f.Pos}
	var reason string
	switch classifyName(name) {
	case nameReserved:
		base.level, reason = SeverityError, "a reserved word in PostgreSQL 17, so every query must quote it"
	case nameAmbiguousType:
		base.level, reason = SeverityError, "a bare type word that does not say which event or value it holds"
	case nameGeneric:
		base.level, reason = SeverityWarning, "a generic name that does not say what is recorded"
	case nameWeaklyAmbiguous:
		base.level, reason = SeverityNote, "weakly ambiguous"
		if categorical >= 2 {
			base.level = SeverityWarning
			reason += fmt.Sprintf(", and %s has %d categorical fields it cannot be told apart from",
				e.Name, categorical)
		}
	default:
		return issue{}, false
	}
	base.measured = fmt.Sprintf("column %s.%s is %s", e.Name, f.Name, reason)
	base.suggestion = fmt.Sprintf("Rename %s to say what it records, for example %s.",
		f.Name, renameCandidates(e, name))
	return base, true
}

func renameCandidates(e *Entity, name string) string {
	stem := Canonical(e.Name)
	switch name {
	case "date", "time", "timestamp", "datetime":
		return "created_at, or <event>_at such as shipped_at"
	case "type", "kind":
		return stem + "_" + name
	case "user":
		return "user_id, or the role the user plays (author_id, assignee_id)"
	case "order":
		return "order_id, or sort_order for a position"
	default:
		return stem + "_" + name
	}
}

// countCategorical counts fields that hold a category: enumerated values, a
// CHECK IN-list, or a name ending in _type, _kind, _category or _status.
func countCategorical(e *Entity) int {
	n := 0
	for i := range e.Fields {
		f := &e.Fields[i]
		lower := strings.ToLower(f.Name)
		if len(f.Enum) > 0 || hasEnum(e, f) || lower == "type" || lower == "kind" ||
			hasAnySuffix(lower, "_type", "_kind", "_category", "_status") {
			n++
		}
	}
	return n
}

func hasAnySuffix(s string, suffixes ...string) bool {
	return slices.ContainsFunc(suffixes, func(x string) bool { return strings.HasSuffix(s, x) })
}

func check55A(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		for _, g := range numberedGroups(c, e) {
			if len(g.numbers) < 3 || g.stem == "address_line" {
				continue
			}
			out = append(out, issue{
				entity: e, field: g.first.Name, pos: g.first.Pos,
				measured: fmt.Sprintf(
					"%s has %d numbered columns %s%d…%s%d%s",
					e.Name,
					len(
						g.numbers,
					),
					g.stem,
					slices.Min(g.numbers),
					g.stem,
					slices.Max(g.numbers),
					g.suffix,
				),
				suggestion: fmt.Sprintf("Replace them with a child table %s_%ss(%s_id REFERENCES "+
					"%s, %s_seq SMALLINT, %s ..., PRIMARY KEY (%s_id, %s_seq)).",
					Canonical(e.Name), g.stem, Canonical(e.Name), e.Name, g.stem, g.stem,
					Canonical(e.Name), g.stem),
			})
		}
		out = append(out, fixedArrays(e)...)
	}
	return out
}

func numberedGroups(c *catalog, e *Entity) []siblingGroup {
	var groups []siblingGroup
	for i := range e.Fields {
		f := &e.Fields[i]
		m := c.rx.numbered.FindStringSubmatch(strings.ToLower(f.Name))
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(m[2])
		if err != nil {
			continue
		}
		stem, suffix := strings.TrimSuffix(m[1], "_"), m[3]
		i := slices.IndexFunc(groups, func(g siblingGroup) bool {
			return g.stem == stem && g.suffix == suffix
		})
		if i < 0 {
			groups = append(groups, siblingGroup{stem: stem, suffix: suffix, first: f})
			i = len(groups) - 1
		}
		groups[i].numbers = append(groups[i].numbers, n)
	}
	return groups
}

// fixedArrays reports Go fixed-size array fields ([3]string), which are
// numbered columns by another name.
func fixedArrays(e *Entity) []issue {
	var out []issue
	for i := range e.Fields {
		f := &e.Fields[i]
		if e.Source != SourceGo || !strings.HasPrefix(f.RawType, "[") ||
			strings.HasPrefix(f.RawType, "[]") {
			continue
		}
		out = append(out, issue{
			entity: e, field: f.Name, pos: f.Pos,
			measured: fmt.Sprintf("%s.%s is a fixed-size array %s, a fixed number of repeated "+
				"columns", e.Name, f.Name, f.RawType),
			suggestion: fmt.Sprintf("Use a slice persisted as a child table keyed by (%s_id, "+
				"seq), so the number of items is data, not schema.", Canonical(e.Name)),
		})
	}
	return out
}
