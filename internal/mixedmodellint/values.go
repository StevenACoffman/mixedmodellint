package mixedmodellint

import (
	"path"
	"strings"
)

// enumValues returns the values f is constrained to, by an enum or a CHECK.
func enumValues(e *Entity, f *Field) []string {
	if len(f.Enum) > 0 {
		return f.Enum
	}
	for _, ch := range e.Checks {
		if strings.EqualFold(ch.Field, f.Name) && len(ch.Enum) > 0 {
			return ch.Enum
		}
	}
	return nil
}

// domainOf returns the domain e belongs to, for the rules about meaning that
// changes at a domain boundary (§2.2). A `domain` annotation wins; otherwise
// the domain is the unit a team already draws a boundary with: the SQL schema
// (public is no domain), the Go package, or the JSON Schema file's directory.
// rule_plan.md also allows configured `domains.<name>.paths`; until mml.yaml
// has a config section, these structural boundaries are the only ones.
//
// Ensures: "" when e has no domain, so an unqualified SQL table never crosses
// a boundary.
func domainOf(e *Entity) string {
	if d, ok := e.Annotations.Get("domain"); ok {
		return d
	}
	_, id, _ := strings.Cut(e.ID, ":")
	switch e.Source {
	case SourceSQL:
		schema, _, qualified := strings.Cut(id, ".")
		if !qualified || schema == "public" {
			return ""
		}
		return schema
	case SourceGo:
		if dot := strings.LastIndex(id, "."); dot > 0 {
			return path.Base(id[:dot])
		}
	case SourceJSONSchema:
		file, _, _ := strings.Cut(id, "#")
		if dir := path.Dir(file); dir != "." {
			return path.Base(dir)
		}
	}
	return ""
}

// snakeName writes a name of any case style in snake_case, so one pattern
// matches a SQL column and the Go field it maps to (ContentHash is
// content_hash).
func snakeName(name string) string {
	return strings.Join(splitWords(name), "_")
}

// levenshtein returns the number of single-character insertions, deletions
// and substitutions that turn a into b, comparing bytes, which is enough for
// the ASCII names and codes it is used on.
func levenshtein(a, b string) int {
	row := make([]int, len(b)+1)
	for j := range row {
		row[j] = j
	}
	for i := 1; i <= len(a); i++ {
		diag := row[0]
		row[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			diag, row[j] = row[j], min(row[j]+1, row[j-1]+1, diag+cost)
		}
	}
	return row[len(b)]
}

// orList joins items as prose: "a", "a or b", "a, b or c".
func orList(items []string) string { return proseList(items, "or") }

// andList joins items as prose: "a", "a and b", "a, b and c".
func andList(items []string) string { return proseList(items, "and") }

func proseList(items []string, conj string) string {
	if len(items) < 2 {
		return strings.Join(items, "")
	}
	return strings.Join(items[:len(items)-1], ", ") + " " + conj + " " + items[len(items)-1]
}

// outsideConventions reports whether e is work the team's modeling
// conventions do not cover: someone else's schema, or declared scratch work.
func outsideConventions(e *Entity) bool {
	life, _ := e.Annotations.Get("lifecycle")
	return foreignSchema(e) || life == "scratch"
}
