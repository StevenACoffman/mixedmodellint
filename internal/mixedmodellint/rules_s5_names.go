package mixedmodellint

import (
	"fmt"
	"iter"
	"strings"
	"unicode"
)

// Case styles a persisted name can be written in. styleNone is a name that
// shows no style, such as a single lowercase word.
const (
	styleNone caseStyle = iota
	styleSnake
	styleScreaming
	styleCamel
	stylePascal
	styleMixed
)

// §5.2-A's thresholds: a style or entity number dominates when more than
// dominantShare of the names that show one use it, judged only over at
// least minStyleNames names; below that there is too little to call a
// convention.
const (
	dominantShare = 0.6
	minStyleNames = 5
)

// caseStyle is how a name separates and capitalizes its words.
type caseStyle int

// nameScope is a group of names that should share one convention: the
// columns or properties of one source kind, or its entity names.
type nameScope struct {
	label string // "SQL column", "JSON property", "SQL table"
	names []scopedName
}

// scopedName is one name in a scope, with what it names.
type scopedName struct {
	entity *Entity
	field  *Field // nil for an entity name
}

func section5NameDesigns() []design {
	return []design{
		{
			id: "5.2-A", title: "Convention conformance profile", strength: Should,
			check: check52A,
		},
		{
			id: "5.2-B", title: "Abbreviation and cryptic-token detector", strength: Should,
			check: check52B,
		},
	}
}

func (s caseStyle) String() string {
	return [...]string{
		"no style", "snake_case", "SCREAMING_CASE", "camelCase", "PascalCase",
		"mixed case",
	}[s]
}

// styleOf classifies name's case style.
func styleOf(name string) caseStyle {
	upper := strings.IndexFunc(name, unicode.IsUpper) >= 0
	lower := strings.IndexFunc(name, unicode.IsLower) >= 0
	switch {
	case strings.Contains(name, "_") && !upper:
		return styleSnake
	case strings.Contains(name, "_") && !lower:
		return styleScreaming
	case strings.ContainsAny(name, "_-"):
		return styleMixed
	case !upper:
		return styleNone
	case !lower:
		return styleScreaming
	case unicode.IsLower(rune(name[0])):
		return styleCamel
	default:
		return stylePascal
	}
}

// inStyle writes words in style s.
func inStyle(words []string, s caseStyle) string {
	switch s {
	case styleScreaming:
		return strings.ToUpper(strings.Join(words, "_"))
	case styleCamel, stylePascal:
		var b strings.Builder
		for i, w := range words {
			if i == 0 && s == styleCamel {
				b.WriteString(w)
				continue
			}
			b.WriteString(strings.ToUpper(w[:1]) + w[1:])
		}
		return b.String()
	default:
		return strings.Join(words, "_")
	}
}

// nameScopes returns the names §5.2-A compares, by scope. Go names follow
// Go's casing whatever the persisted name is, so only SQL and JSON Schema
// names are judged; someone else's schema and scratch work keep their own.
func (c *catalog) nameScopes() []nameScope {
	scopes := map[string]*nameScope{}
	var order []string
	add := func(label string, n scopedName) {
		if scopes[label] == nil {
			scopes[label] = &nameScope{label: label}
			order = append(order, label)
		}
		scopes[label].names = append(scopes[label].names, n)
	}
	for _, e := range c.entities {
		if e.Source == SourceGo || outsideConventions(e) {
			continue
		}
		kind := map[SourceKind]string{SourceSQL: "SQL", SourceJSONSchema: "JSON Schema"}[e.Source]
		add(kind+" entity", scopedName{entity: e})
		for i := range e.Fields {
			add(kind+" field", scopedName{entity: e, field: &e.Fields[i]})
		}
	}
	out := make([]nameScope, 0, len(order))
	for _, label := range order {
		out = append(out, *scopes[label])
	}
	return out
}

func (n *scopedName) name() string {
	if n.field != nil {
		return n.field.Name
	}
	return n.entity.Name
}

// check52A reports names that break their scope's convention (rule_plan.md
// §5.2-A): a case style other than the dominant one, a table named in the
// other grammatical number, and two fields of one entity spelled
// differently that are the same words (OrderDate, order_date). Without a
// dominant style there is no convention to break, so nothing is reported;
// rule_plan.md's note asking for a configured convention waits for a
// config section.
func check52A(c *catalog) []issue {
	var out []issue
	collided := map[*Field]bool{}
	for _, e := range c.entities {
		for f, first := range spellingCollisions(e) {
			collided[f] = true
			out = append(out, collisionIssue(e, f, first))
		}
	}
	for _, sc := range c.nameScopes() {
		out = append(out, styleIssues(&sc, collided)...)
		out = append(out, numberIssues(&sc)...)
	}
	return out
}

// styleIssues reports the names of sc that break its dominant style, except
// fields a spelling collision already reports.
func styleIssues(sc *nameScope, collided map[*Field]bool) []issue {
	style, share, ok := dominantStyle(sc)
	if !ok {
		return nil
	}
	var out []issue
	for i := range sc.names {
		if collided[sc.names[i].field] {
			continue
		}
		if is, ok := styleIssue(sc, &sc.names[i], style, share); ok {
			out = append(out, is)
		}
	}
	return out
}

// dominantStyle returns the style more than dominantShare of sc's names use,
// among those that show one.
func dominantStyle(sc *nameScope) (caseStyle, float64, bool) {
	counts := map[caseStyle]int{}
	total := 0
	for i := range sc.names {
		if s := styleOf(sc.names[i].name()); s != styleNone {
			counts[s]++
			total++
		}
	}
	for s, n := range counts {
		if total >= minStyleNames && float64(n) > dominantShare*float64(total) {
			return s, float64(n) / float64(total), true
		}
	}
	return styleNone, 0, false
}

// styleIssue reports n when it shows a style other than the dominant one.
func styleIssue(sc *nameScope, n *scopedName, style caseStyle, share float64) (issue, bool) {
	name := n.name()
	s := styleOf(name)
	if s == styleNone || s == style {
		return issue{}, false
	}
	to := inStyle(splitWords(name), style)
	is := issue{
		entity: n.entity, pos: n.entity.Pos,
		measured: fmt.Sprintf("%s is %s, but %.0f%% of the model's %s names are %s", name, s,
			share*100, sc.label, style),
		suggestion: fmt.Sprintf("Rename it to %s (§5.2).", to),
	}
	if n.field != nil {
		is.field, is.pos = n.field.Name, n.field.Pos
		is.measured = n.entity.Name + "." + is.measured
		if n.entity.Source == SourceSQL {
			is.suggestion = fmt.Sprintf("Rename it to %s: ALTER TABLE %s RENAME COLUMN %s TO "+
				"%s (§5.2).", to, n.entity.Name, name, to)
		}
	}
	return is, true
}

// numberIssues reports table names in the grammatical number most of sc's
// table names do not use: orders beside customer.
func numberIssues(sc *nameScope) []issue {
	var plural, singular []*Entity
	for i := range sc.names {
		e := sc.names[i].entity
		if sc.names[i].field != nil || e.Kind != KindTable {
			continue
		}
		if pluralName(e.Name) {
			plural = append(plural, e)
		} else {
			singular = append(singular, e)
		}
	}
	total := float64(len(plural) + len(singular))
	var odd []*Entity
	var want string
	switch {
	case total < minStyleNames:
		return nil
	case float64(len(plural)) > dominantShare*total:
		odd, want = singular, "plural"
	case float64(len(singular)) > dominantShare*total:
		odd, want = plural, "singular"
	default:
		return nil
	}
	out := make([]issue, 0, len(odd))
	for _, e := range odd {
		out = append(out, issue{
			entity: e,
			measured: fmt.Sprintf("%s is not %s, though most %s names are", e.Name, want,
				sc.label),
			suggestion: fmt.Sprintf("Name it in the %s like the others (§5.2).", want),
		})
	}
	return out
}

// pluralName reports whether name's last word is plural.
func pluralName(name string) bool {
	ws := splitWords(name)
	return len(ws) > 0 && singular(ws[len(ws)-1]) != ws[len(ws)-1]
}

// spellingCollisions yields each field of e that repeats an earlier
// field's words spelled differently (placed_at, then PlacedAt), which
// readers and tools take for two attributes, with the earlier field's name.
func spellingCollisions(e *Entity) iter.Seq2[*Field, string] {
	return func(yield func(*Field, string) bool) {
		first := map[string]string{}
		for i := range e.Fields {
			f := &e.Fields[i]
			key := strings.Join(splitWords(f.Name), "")
			prev, seen := first[key]
			if !seen {
				first[key] = f.Name
			} else if prev != f.Name && !yield(f, prev) {
				return
			}
		}
	}
}

// collisionIssue reports f, which spells the words of e's field first again.
func collisionIssue(e *Entity, f *Field, first string) issue {
	return issue{
		entity: e, field: f.Name, pos: f.Pos,
		measured: fmt.Sprintf("%s.%s and %s.%s are the same words spelled two ways, so they "+
			"read as two attributes", e.Name, first, e.Name, f.Name),
		suggestion: "Keep one spelling, in the model's convention, and merge or rename the " +
			"other (§5.2).",
	}
}

// check52B reports cryptic abbreviations in names (rule_plan.md §5.2-B):
// a token from the bundled abbreviation table, such as qty or cust, or a
// name that is a few letters and a number (c_3). Spelling the word out is
// the fix. rule_plan.md also flags any short token missing from an English
// dictionary; with no dictionary bundled, only the table's tokens are known
// to be cryptic.
func check52B(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		if outsideConventions(e) {
			continue
		}
		if is, ok := c.crypticIssue(e, nil); ok {
			out = append(out, is)
		}
		for i := range e.Fields {
			if is, ok := c.crypticIssue(e, &e.Fields[i]); ok {
				out = append(out, is)
			}
		}
	}
	return out
}

// crypticIssue applies §5.2-B to e's name, or to f's when f is not nil.
func (c *catalog) crypticIssue(e *Entity, f *Field) (issue, bool) {
	name, where, pos := e.Name, e.Name, e.Pos
	if f != nil {
		name, where, pos = f.Name, e.Name+"."+f.Name, f.Pos
	}
	words := splitWords(name)
	var found, spelled []string
	for i, w := range words {
		if long, ok := c.rx.abbrev[w]; ok {
			found = append(found, w)
			words[i] = long
		}
		spelled = append(spelled, words[i])
	}
	is := issue{entity: e, pos: pos}
	if f != nil {
		is.field = f.Name
	}
	switch {
	case len(found) > 0:
		is.measured = fmt.Sprintf("%s uses the abbreviation %s", where, andList(found))
		is.suggestion = fmt.Sprintf("Spell it out as %s (§5.2).",
			inStyle(spelled, styleOfOr(name, styleSnake)))
	case c.rx.crypticName.MatchString(name):
		is.measured = fmt.Sprintf("%s is a few letters and a number, which says nothing "+
			"about what it holds", where)
		is.suggestion = "Name it for what it holds, in the business's words (§5.2)."
	default:
		return issue{}, false
	}
	return is, true
}

// styleOfOr returns name's style, or def when it shows none.
func styleOfOr(name string, def caseStyle) caseStyle {
	if s := styleOf(name); s != styleNone && s != styleMixed {
		return s
	}
	return def
}
