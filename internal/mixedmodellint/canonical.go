package mixedmodellint

import (
	"strings"
	"unicode"

	"github.com/jinzhu/inflection"
)

// Canonical folds an entity or field name to the form used for cross-source
// linking: snake_case, lowercase, with the configured affixes removed and the
// last word singularized. The Go struct OrderLine, the table order_lines and
// the JSON Schema definition "Order-Line" all fold to "order_line".
//
// Requires: nothing; any string is accepted.
// Ensures:  Canonical(Canonical(s)) == Canonical(s), and the result does not
// depend on whether s was written in camelCase, PascalCase, snake_case,
// kebab-case or with spaces.
func Canonical(name string) string {
	words := splitWords(name)
	words = dropAffixes(words)
	if len(words) == 0 {
		return ""
	}
	words[len(words)-1] = singular(words[len(words)-1])
	return strings.Join(words, "_")
}

// splitWords breaks a name into lowercase words at case changes, digits that
// follow letters, and any non-alphanumeric separator. "HTTPServerID" yields
// http, server, id; "order_lines2" yields order, lines2.
func splitWords(name string) []string {
	var words []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			words = append(words, string(cur))
			cur = cur[:0]
		}
	}
	runes := []rune(name)
	for i, r := range runes {
		switch {
		case !unicode.IsLetter(r) && !unicode.IsDigit(r):
			flush()
			continue
		case isUpper(r) && i > 0 && startsWord(runes, i):
			flush()
		}
		// Per-rune lowering keeps one rune per rune; strings.ToLower can turn
		// one letter into a letter plus a combining mark, which would split
		// differently on a second pass and break idempotence.
		cur = append(cur, unicode.ToLower(r))
	}
	flush()
	return words
}

// startsWord reports whether the upper-case rune at i begins a new word: it
// follows a lower-case letter or digit ("orderLine"), or it ends an acronym
// that a lower-case letter continues ("HTTPServer" splits before "Server").
func startsWord(runes []rune, i int) bool {
	prev := runes[i-1]
	if unicode.IsLower(prev) || unicode.IsDigit(prev) {
		return true
	}
	return isUpper(prev) && i+1 < len(runes) && unicode.IsLower(runes[i+1])
}

// isUpper reports whether lowering r changes it. unicode.IsUpper is not used,
// because some upper-case letters (U+1D400 MATHEMATICAL BOLD CAPITAL A) have
// no lower-case form: they would split a word on the first pass but not on
// the second, and Canonical would not be idempotent.
func isUpper(r rune) bool {
	return unicode.ToLower(r) != r
}

// dropAffixes removes words that name the representation rather than the
// thing: tbl, dto, model, record, entity, in either number ("order_models" is
// "order"). A name made only of affixes keeps them, so "Model" is still
// "model".
func dropAffixes(words []string) []string {
	kept := make([]string, 0, len(words))
	for _, w := range words {
		switch singular(w) {
		case "tbl", "dto", "model", "record", "entity":
		default:
			kept = append(kept, w)
		}
	}
	if len(kept) == 0 {
		return words
	}
	return kept
}

// singular returns the singular of an English noun, using inflection's
// rule tables, which also know irregular plurals (people, indices, children).
// A word that would singularize to nothing ("s") is kept as it is, so a name
// never loses a word. The tables are global in inflection; this package only
// reads them and never calls its Add or Set functions.
func singular(w string) string {
	if s := inflection.Singular(w); s != "" {
		return s
	}
	return w
}
