package mixedmodellint

import (
	"fmt"
	"slices"
	"strings"
)

// minVariantLength keeps short codes (US, UK; low, lo) out of §5.4-B's
// near-synonym check, where one letter is most of the word.
const minVariantLength = 4

func section5CodeDesigns() []design {
	return []design{
		{
			id: "5.4-A", title: "Unconstrained categorical detector", strength: Should,
			check: check54A,
		},
		{
			id: "5.4-B", title: "Cross-form enum agreement and synonym collision",
			strength: Should, check: check54B,
		},
	}
}

// check54A reports a categorical string field, one named for a status,
// type, reason, code and the like, with no allowed list: no enum, CHECK or
// reference (rule_plan.md §5.4-A). A key is exempt: a lookup table's code
// column is the allowed list. Classifications such as category and
// tier are §2.3-A's, and regulated codes such as country_code §2.7-A's, so
// they are left to those designs. Go is not checked: a named string type
// with a const block is not loaded, so a Go field's constraint cannot be
// seen.
func check54A(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		if e.Source == SourceGo || e.Kind == KindView || outsideConventions(e) {
			continue
		}
		for i := range e.Fields {
			if is, ok := c.unconstrainedCode(e, &e.Fields[i]); ok {
				out = append(out, is)
			}
		}
	}
	return out
}

// unconstrainedCode applies §5.4-A to one field.
func (c *catalog) unconstrainedCode(e *Entity, f *Field) (issue, bool) {
	if f.Type != TypeString || !c.rx.categorical.MatchString(snakeName(f.Name)) ||
		len(enumValues(e, f)) > 0 || isKeyOrRef(e, f.Name) || c.declaresReference(e, f.Name) {
		return issue{}, false
	}
	if _, ok := c.classificationTerm(f); ok {
		return issue{}, false
	}
	if _, ok := c.regulatedConcept(f); ok {
		return issue{}, false
	}
	fix := fmt.Sprintf("CHECK (%s IN ('<value1>', '<value2>', ...))", f.Name)
	if e.Source == SourceJSONSchema {
		fix = `"enum": ["<value1>", "<value2>", ...]`
	}
	return issue{
		entity: e, field: f.Name, pos: f.Pos,
		measured: fmt.Sprintf("%s.%s is a coded value with no allowed list, so any string "+
			"can be written to it", e.Name, f.Name),
		suggestion: fmt.Sprintf("Add %s, or reference a lookup table of the codes; waive "+
			"this with a reason if it is free text (§5.4).", fix),
	}, true
}

// check54B reports allowed lists that disagree or hold one code several
// ways (rule_plan.md §5.4-B): the forms of one field allowing different
// values, and two values in one list that are equal once case and
// separators are ignored (errors), or near-synonyms, one a prefix of the
// other or a letter or two apart (warnings).
func check54B(c *catalog) []issue {
	var out []issue
	for _, g := range c.logicalGroups() {
		for _, p := range linkedPairs(g) {
			if is, ok := enumDisagreement(&p); ok {
				out = append(out, is)
			}
		}
	}
	for _, e := range c.entities {
		if outsideConventions(e) {
			continue
		}
		for i := range e.Fields {
			out = append(out, variantIssues(e, &e.Fields[i])...)
		}
	}
	return out
}

// normalCode folds a code for comparison: case, surrounding space and the
// separators _ and - do not distinguish codes.
func normalCode(v string) string {
	return strings.NewReplacer("_", "", "-", "", " ", "").Replace(
		strings.ToLower(strings.TrimSpace(v)))
}

// enumDisagreement reports when two forms of one field allow different
// values.
func enumDisagreement(p *memberPair) (issue, bool) {
	a, b := enumValues(p.a, p.aField), enumValues(p.b, p.bField)
	if len(a) == 0 || len(b) == 0 {
		return issue{}, false
	}
	onlyA, onlyB := missingCodes(a, b), missingCodes(b, a)
	if len(onlyA) == 0 && len(onlyB) == 0 {
		return issue{}, false
	}
	var gaps []string
	if len(onlyA) > 0 {
		gaps = append(gaps, fmt.Sprintf("%s is missing from the %s", quoteList(onlyA),
			memberName(p.b)))
	}
	if len(onlyB) > 0 {
		gaps = append(gaps, fmt.Sprintf("%s is missing from the %s", quoteList(onlyB),
			memberName(p.a)))
	}
	return issue{
		level: SeverityError, entity: p.a, field: p.aField.Name, pos: p.aField.Pos,
		measured: fmt.Sprintf("%s.%s and %s.%s allow different values: %s", p.a.Name,
			p.aField.Name, p.b.Name, p.bField.Name, strings.Join(gaps, "; ")),
		suggestion: "Make every form allow the same codes, generating them from one list " +
			"if you can (§5.4).",
	}, true
}

// missingCodes returns the values of a that b lacks, compared as codes.
func missingCodes(a, b []string) []string {
	var out []string
	for _, v := range a {
		if !slices.ContainsFunc(b, func(w string) bool { return normalCode(w) == normalCode(v) }) {
			out = append(out, v)
		}
	}
	return out
}

// variantIssues reports values of f's allowed list that are one code
// written several ways: an error when they are equal as codes, a warning
// when they are near-synonyms.
func variantIssues(e *Entity, f *Field) []issue {
	values := enumValues(e, f)
	var same, near [][2]string
	for i, a := range values {
		for _, b := range values[i+1:] {
			switch {
			case normalCode(a) == normalCode(b):
				same = append(same, [2]string{a, b})
			case nearSynonyms(normalCode(a), normalCode(b)):
				near = append(near, [2]string{a, b})
			}
		}
	}
	var out []issue
	if len(same) > 0 {
		is := variantIssue(e, f, same, "are the same code written differently")
		is.level = SeverityError
		out = append(out, is)
	}
	if len(near) > 0 {
		out = append(out, variantIssue(e, f, near, "look like variants of one code"))
	}
	return out
}

// nearSynonyms reports whether two distinct normalized codes look like
// spellings of one: one a prefix of the other (defect, defective), or the
// longer the shorter with a letter inserted (two for long codes: canceled,
// cancelled). A changed letter makes a different word as often as a variant
// (shipped, skipped), so only insertions count: the edit distance is then
// exactly the difference in length.
func nearSynonyms(a, b string) bool {
	if len(a) > len(b) {
		a, b = b, a
	}
	if len(a) < minVariantLength {
		return false
	}
	limit := 1
	if len(b) >= 2*minVariantLength {
		limit = 2
	}
	grown := len(b) - len(a)
	return strings.HasPrefix(b, a) || grown <= limit && levenshtein(a, b) == grown
}

func variantIssue(e *Entity, f *Field, pairs [][2]string, what string) issue {
	listed := make([]string, len(pairs))
	for i, p := range pairs {
		listed[i] = fmt.Sprintf("%q and %q", p[0], p[1])
	}
	return issue{
		entity: e, field: f.Name, pos: f.Pos,
		measured: fmt.Sprintf("in the allowed list of %s.%s, %s %s", e.Name, f.Name,
			strings.Join(listed, "; "), what),
		suggestion: "Keep one canonical code for each and map the others to it in a lookup " +
			"table; waive this with a reason if they are distinct codes (§5.4).",
	}
}
