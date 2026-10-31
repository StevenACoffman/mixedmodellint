package mixedmodellint

import (
	"strings"
	"unicode"
)

// Annotation is one declared value, such as a grain statement, with the
// position it was declared at so a finding about it can point there.
type Annotation struct {
	Value string
	Pos   Position
}

// Annotations maps an annotation key, such as "grain", to its value. The
// same keys are read from Go directives and tags, SQL comments, JSON Schema
// x-mml-* keywords and the mml.yaml sidecar (rule_plan.md §2).
type Annotations map[string]Annotation

// Get returns the value of key and true. A missing key, an empty value and
// the placeholder "TODO" all return false, so a scaffolded stub never
// satisfies a rule.
func (a Annotations) Get(key string) (string, bool) {
	v, ok := a[key]
	if !ok {
		return "", false
	}
	text := strings.TrimSpace(v.Value)
	if text == "" || strings.EqualFold(text, "TODO") {
		return "", false
	}
	return text, true
}

// Has reports whether key is present with a value other than TODO.
func (a Annotations) Has(key string) bool {
	_, ok := a.Get(key)
	return ok
}

// ParseLines reads annotations from free text, one per line, in either of the
// two line syntaxes: "@key: value" (SQL COMMENT ON) and "mml:key value" (a Go
// doc directive or a SQL "-- mml:" comment, with the comment marker already
// removed). Other lines are ignored, so annotations can sit inside an
// ordinary description. Line numbers in the result count from pos.Line.
func ParseLines(text string, pos Position) Annotations {
	out := Annotations{}
	for i, line := range strings.Split(text, "\n") {
		key, value, ok := parseLine(strings.TrimSpace(line))
		if !ok {
			continue
		}
		p := pos
		if p.Line > 0 {
			p.Line += i
		}
		out[key] = Annotation{Value: value, Pos: p}
	}
	return out
}

// ParseTag reads the field-level Go struct tag syntax,
// `mml:"null=not_applicable;unit=USD;pk"`. A key with no "=" is a flag and
// gets the value "true".
func ParseTag(tag string, pos Position) Annotations {
	out := Annotations{}
	for part := range strings.SplitSeq(tag, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, value, found := strings.Cut(part, "=")
		if !found {
			value = "true"
		}
		out[strings.TrimSpace(key)] = Annotation{Value: strings.TrimSpace(value), Pos: pos}
	}
	return out
}

// splitParams separates a value's free text from its key=value parameters:
// `one row per order fp=3f9a` has text "one row per order" and fp=3f9a.
// A parameter value may be quoted with " or ' to contain spaces, as in
// `7.1 reason="vendor table"`.
func splitParams(value string) (text string, params map[string]string) {
	params = map[string]string{}
	var words []string
	for _, tok := range tokenize(value) {
		key, v, found := strings.Cut(tok, "=")
		if found && isIdent(key) {
			params[key] = unquote(v)
			continue
		}
		words = append(words, tok)
	}
	return strings.Join(words, " "), params
}

func parseLine(line string) (key, value string, ok bool) {
	switch {
	case strings.HasPrefix(line, "@"):
		key, value, ok = strings.Cut(line[1:], ":")
	case strings.HasPrefix(line, "mml:"):
		rest := line[len("mml:"):]
		key, value, _ = strings.Cut(rest, " ")
		ok = true
	default:
		return "", "", false
	}
	key = strings.TrimSpace(key)
	if !ok || !isIdent(key) {
		return "", "", false
	}
	return key, strings.TrimSpace(value), true
}

// tokenize splits on spaces, keeping a quoted run (after "=") as part of its
// token: `a reason="b c"` is ["a", `reason="b c"`].
func tokenize(s string) []string {
	var toks []string
	var cur strings.Builder
	var quote rune
	for _, r := range s {
		switch {
		case quote != 0:
			cur.WriteRune(r)
			if r == quote {
				quote = 0
			}
		case r == '"' || r == '\'':
			quote = r
			cur.WriteRune(r)
		case unicode.IsSpace(r):
			if cur.Len() > 0 {
				toks = append(toks, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		toks = append(toks, cur.String())
	}
	return toks
}

func unquote(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1]
	}
	return v
}

// isIdent reports whether s is an annotation key: letters, digits, '_' and
// '-', starting with a letter. "value-object" and "max-items" are keys.
func isIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case unicode.IsLetter(r):
		case i > 0 && (unicode.IsDigit(r) || r == '_' || r == '-'):
		default:
			return false
		}
	}
	return true
}
