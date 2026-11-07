package mixedmodellint

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"
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
		{id: "5.3-A", title: "Null-meaning contract", strength: Must, check: check53A},
		{
			id:       "5.1-A",
			title:    "Reserved-word and bare-type lexicon",
			strength: Must,
			check:    check51A,
		},
		{id: "5.1-B", title: "Time-column event naming", strength: Must, check: check51B},
		{
			id: "5.3-B", title: "NULL-unsafe aggregation", strength: Must, check: check53B,
		},
		{
			id: "5.3-C", title: "Cross-form nullability consistency", strength: Must,
			check: check53C,
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
			if is, ok := nameIssue(c.rx, e, f, categorical); ok {
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
func nameIssue(rx *lexicon, e *Entity, f *Field, categorical int) (issue, bool) {
	name := strings.ToLower(f.Name)
	base := issue{entity: e, field: f.Name, pos: f.Pos}
	var reason string
	switch rx.classifyName(name) {
	case nameReserved:
		base.level, reason = SeverityError, "a reserved word in PostgreSQL, so every query must quote it"
	case nameSQLReserved:
		base.level, reason = SeverityWarning,
			"a reserved word in SQL:2016, so portable SQL and other databases must quote it"
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

// check51B applies §5.1-B: a date or time column's name must end in a time
// suffix and say, before it, which event it records. A bare type word (date,
// timestamp) is left to 5.1-A, which already reports it as an error, and a
// field with a declared time= role names its event there.
func check51B(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		if thirdParty(e) {
			continue
		}
		for i := range e.Fields {
			f := &e.Fields[i]
			if (!isTemporal(f.Type) && f.Type != TypeTime) || f.Annotations.Has("time") ||
				c.rx.classifyName(strings.ToLower(f.Name)) != nameOK {
				continue
			}
			if problem := eventNameProblem(c.rx, f.Name); problem != "" {
				out = append(out, issue{
					entity: e, field: f.Name, pos: f.Pos,
					measured: fmt.Sprintf("%s.%s is a %s but its name %s", e.Name, f.Name,
						f.Type, problem),
					suggestion: fmt.Sprintf("Rename %s to <event>_%s, for example %s, so "+
						"the name says which event it records (§5.1).", f.Name,
						timeSuffixFor(f.Type), eventNameCandidates(f.Type)),
				})
			}
		}
	}
	return out
}

// eventNameProblem says how name falls short of §5.1-B, or returns "": its
// last word must be at, on, date, time or ts, and a word before it an event
// word, either listed in lexicon/event_verbs.txt or a regular past
// participle ending in -ed. A bound of a named period (period_start,
// valid_to) also passes: the period is what it records, and §8.5-B and
// §9.3-B ask for exactly those names.
func eventNameProblem(rx *lexicon, name string) string {
	ws := splitWords(name)
	last := ""
	if len(ws) >= 2 {
		last = ws[len(ws)-1]
	}
	switch last {
	case "start", "end", "from", "to", "until":
		return ""
	case "at", "on", "date", "time", "ts":
	default:
		return "does not end in _at, _on, _date, _time or _ts after the event it records"
	}
	for _, w := range ws[:len(ws)-1] {
		if rx.eventWords[w] || (len(w) > 3 && strings.HasSuffix(w, "ed")) {
			return ""
		}
	}
	return fmt.Sprintf("does not say which event it records: %s is not an event word",
		strings.Join(ws[:len(ws)-1], "_"))
}

// timeSuffixFor is the suffix §5.1-B suggests for a type: _at for an
// instant, _on for a calendar date, _time for a time of day.
func timeSuffixFor(t LogicalType) string {
	switch t {
	case TypeDate:
		return "on"
	case TypeTime:
		return "time"
	default:
		return "at"
	}
}

func eventNameCandidates(t LogicalType) string {
	suffix := timeSuffixFor(t)
	return fmt.Sprintf("created_%s, shipped_%s or expires_%s", suffix, suffix, suffix)
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

// nullMeanings are the values null= may take (rule_plan.md §5.3-A), plus
// open, which §9.3-B asks an interval's nullable end to declare: the
// interval has not ended yet.
func nullMeanings() []string {
	return []string{"missing", "unknown", "not_applicable", "zero", "forbidden", "open"}
}

func check53A(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		if e.Kind == KindView {
			continue
		}
		for i := range e.Fields {
			f := &e.Fields[i]
			if is, ok := nullMeaningIssue(e, f); ok {
				out = append(out, is)
			}
		}
	}
	return out
}

// nullMeaningIssue applies §5.3-A to one field: a nullable field must say
// what NULL means, not_applicable must name the discriminator that decides
// it, and the declaration must not contradict the structure.
func nullMeaningIssue(e *Entity, f *Field) (issue, bool) {
	if containsFold(e.PrimaryKey, f.Name) {
		return issue{}, false
	}
	meaning, declared := f.Annotations.Get("null")
	is := issue{entity: e, field: f.Name, pos: f.Pos}
	switch {
	case !declared && f.Nullable:
		is.measured = fmt.Sprintf("%s.%s is nullable but does not say what NULL means", e.Name,
			f.Name)
	case !declared:
		return issue{}, false
	case !slices.Contains(nullMeanings(), meaning):
		is.measured = fmt.Sprintf("%s.%s declares null=%s, which is not a meaning", e.Name,
			f.Name, meaning)
	case meaning == "forbidden" && f.Nullable:
		is.measured = fmt.Sprintf("%s.%s declares null=forbidden but allows NULL", e.Name, f.Name)
	case meaning == "not_applicable":
		return notApplicableIssue(e, f)
	default:
		return issue{}, false
	}
	is.suggestion = nullHint(e, f)
	return is, true
}

// notApplicableIssue checks that null=not_applicable names, in null_when=,
// a field of e that decides when the value does not apply.
func notApplicableIssue(e *Entity, f *Field) (issue, bool) {
	when, ok := f.Annotations.Get("null_when")
	if ok {
		if _, exists := e.Field(predicateField(when)); exists {
			return issue{}, false
		}
	}
	return issue{
		entity: e, field: f.Name, pos: f.Pos,
		measured: fmt.Sprintf("%s.%s is null when not applicable, but null_when does not name "+
			"a field of %s", e.Name, f.Name, e.Name),
		suggestion: fmt.Sprintf("Add null_when: <predicate> naming the field that decides it, "+
			"for example %s (§5.3).", discriminatorHint(e)),
	}, true
}

// predicateField returns the field a predicate such as "order_type =
// 'digital'" tests: its first identifier.
func predicateField(pred string) string {
	end := strings.IndexFunc(pred, func(r rune) bool {
		return r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	if end < 0 {
		return pred
	}
	return pred[:end]
}

func nullHint(e *Entity, f *Field) string {
	return fmt.Sprintf("Declare null=missing|unknown|not_applicable|zero on %s; for "+
		"not_applicable also null_when naming the discriminator (e.g. %s). If NULL should "+
		"never happen, make it NOT NULL (§5.3).", f.Name, discriminatorHint(e))
}

// discriminatorHint proposes a predicate on a field named like a type or
// kind, or a placeholder.
func discriminatorHint(e *Entity) string {
	for i := range e.Fields {
		ws := splitWords(e.Fields[i].Name)
		if len(ws) > 0 && (ws[len(ws)-1] == "type" || ws[len(ws)-1] == "kind") {
			return e.Fields[i].Name + " = '<value>'"
		}
	}
	return "<field> = '<value>'"
}

// check53C applies §5.3-C: fields that link across sources must agree on
// whether they may be NULL. The finding is on the member that rejects NULL,
// where a NULL from the other breaks. Reference fields are §6.2-C's.
func check53C(c *catalog) []issue {
	var out []issue
	for _, g := range c.logicalGroups() {
		for _, p := range linkedPairs(g) {
			if p.aField.Nullable == p.bField.Nullable || groupReference(g, p.aField) {
				continue
			}
			strict, sf, loose, lf := p.a, p.aField, p.b, p.bField
			if sf.Nullable {
				strict, sf, loose, lf = p.b, p.bField, p.a, p.aField
			}
			out = append(out, issue{
				entity: strict, field: sf.Name, pos: sf.Pos,
				measured: fmt.Sprintf("%s.%s may be NULL, but %s.%s, its form in %s, may "+
					"not; there a NULL becomes %s", loose.Name, lf.Name, strict.Name, sf.Name,
					memberName(strict), nullBreaks(strict)),
				suggestion: fmt.Sprintf("Align them: make %s.%s %s, or make %s.%s NOT NULL "+
					"(required) if it is never NULL (§5.3).", strict.Name, sf.Name,
					acceptsNull(strict), loose.Name, lf.Name),
			})
		}
	}
	return out
}

// nullBreaks says what a NULL turns into in a member that does not allow
// one.
func nullBreaks(e *Entity) string {
	switch e.Source {
	case SourceGo:
		return "a scan error with database/sql, or the zero value with some ORMs"
	case SourceJSONSchema:
		return "a payload that fails validation, since the property is required"
	default:
		return "a failed write, since the column is NOT NULL"
	}
}

// acceptsNull says how a member of e's source allows NULL.
func acceptsNull(e *Entity) string {
	switch e.Source {
	case SourceGo:
		return "a pointer or sql.Null* field"
	case SourceJSONSchema:
		return "optional or nullable"
	default:
		return "nullable"
	}
}
