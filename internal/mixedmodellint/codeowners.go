package mixedmodellint

import (
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// Codeowners is a parsed CODEOWNERS file: its rules in file order. GitHub
// gives a path the owners of the last rule that matches it.
type Codeowners struct {
	Rules []CodeownersRule
}

// CodeownersRule is one line of CODEOWNERS: a gitignore-style path pattern
// and the principals (@org/team, @user or an email address) it assigns. A
// rule with no owners leaves the paths it matches unowned.
type CodeownersRule struct {
	Pattern string
	Owners  []string
	match   *regexp.Regexp
}

// ParseCodeowners parses a CODEOWNERS file. Comments and blank lines are
// skipped; a line is a pattern followed by its owners.
func ParseCodeowners(text string) *Codeowners {
	co := &Codeowners{}
	for line := range strings.Lines(text) {
		line, _, _ = strings.Cut(line, "#")
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		co.Rules = append(co.Rules, CodeownersRule{
			Pattern: fields[0], Owners: fields[1:], match: codeownersPattern(fields[0]),
		})
	}
	return co
}

// Principals returns every owner any rule names, once each, in file order.
func (co *Codeowners) Principals() []string {
	out := []string{}
	for _, r := range co.Rules {
		for _, o := range r.Owners {
			if !slices.Contains(out, o) {
				out = append(out, o)
			}
		}
	}
	return out
}

// Owners returns the owners CODEOWNERS assigns to path, a slash-separated
// path relative to the repository root, and whether any rule matches it.
//
// Requires: path has no leading slash.
// Ensures:  the owners are those of the last matching rule, as GitHub
// resolves them; ok is false when no rule matches.
func (co *Codeowners) Owners(path string) (owners []string, ok bool) {
	for i := len(co.Rules) - 1; i >= 0; i-- {
		if co.Rules[i].match.MatchString(path) {
			return co.Rules[i].Owners, true
		}
	}
	return nil, false
}

// codeownersPattern compiles a CODEOWNERS pattern with gitignore's rules: a
// leading slash, or a slash before the end, anchors it at the root; *
// matches within a path segment and ** across segments; a trailing slash
// matches only a directory; and a pattern matching a directory matches
// everything in it.
func codeownersPattern(p string) *regexp.Regexp {
	dirOnly := strings.HasSuffix(p, "/")
	p = strings.TrimSuffix(p, "/")
	anchored := strings.Contains(p, "/")
	p = strings.TrimPrefix(p, "/")
	var b strings.Builder
	b.WriteString("^")
	if !anchored {
		b.WriteString("(?:.*/)?")
	}
	for i := 0; i < len(p); {
		r, size := utf8.DecodeRuneInString(p[i:])
		switch {
		case strings.HasPrefix(p[i:], "**/"):
			b.WriteString("(?:.*/)?")
			size = 3
		case strings.HasPrefix(p[i:], "**"):
			b.WriteString(".*")
			size = 2
		case r == '*':
			b.WriteString("[^/]*")
		case r == '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
		i += size
	}
	if dirOnly {
		b.WriteString("/.*$")
	} else {
		b.WriteString("(?:/.*)?$")
	}
	return regexp.MustCompile(b.String())
}
