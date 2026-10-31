package mixedmodellint

import (
	"fmt"
	"strings"
)

func section9Designs() []design {
	return []design{
		{id: "9.2-A", title: "Native temporal types", strength: Must, check: check92A},
	}
}

// temporalName reports whether a field name says it holds a date or instant:
// its last word is at, on, date, time, ts, timestamp, dob, birthday or
// expires, or it is valid_from/valid_to. Word matching keeps candidate,
// runtime and lifetime out; duration names are excluded.
func temporalName(c *catalog, name string) bool {
	if c.rx.duration.MatchString(name) {
		return false
	}
	ws := splitWords(name)
	if len(ws) == 0 {
		return false
	}
	switch ws[len(ws)-1] {
	case "at", "on", "date", "time", "ts", "timestamp", "dob", "birthday", "expires":
		return true
	}
	lower := strings.ToLower(name)
	return lower == "valid_from" || lower == "valid_to"
}

func check92A(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		for i := range e.Fields {
			f := &e.Fields[i]
			if is, ok := temporalTypeIssue(c, e, f); ok {
				out = append(out, is)
			}
		}
	}
	return out
}

func temporalTypeIssue(c *catalog, e *Entity, f *Field) (issue, bool) {
	base := issue{entity: e, field: f.Name, pos: f.Pos}
	switch {
	case f.Type == TypeTimestampNaive:
		base.measured = fmt.Sprintf("%s.%s is %s, a timestamp without a time zone, so the "+
			"instant it names depends on the writer's zone", e.Name, f.Name, f.RawType)
		base.suggestion = fmt.Sprintf("Store it as timestamptz: ALTER TABLE %s ALTER COLUMN %s "+
			"TYPE timestamptz USING %s AT TIME ZONE '<zone the values were written in>';",
			e.Name, f.Name, f.Name)
		return base, true
	case !temporalName(c, f.Name):
		return issue{}, false
	case f.Type == TypeString:
		base.measured = fmt.Sprintf("%s.%s is named as a date or time but is %s", e.Name,
			f.Name, rawOr(f, "a string"))
		base.suggestion = stringTemporalFix(e, f)
		return base, true
	case f.Type == TypeInt && !f.Annotations.Has("encoding"):
		base.measured = fmt.Sprintf("%s.%s is named as a date or time but is an integer "+
			"with no declared epoch encoding", e.Name, f.Name)
		base.suggestion = fmt.Sprintf("Store it as a native timestamp type, or declare its "+
			"encoding: %s.", fieldAnnotationHint(e, f, "encoding", "unix_ms"))
		return base, true
	default:
		return issue{}, false
	}
}

func stringTemporalFix(e *Entity, f *Field) string {
	switch e.Source {
	case SourceJSONSchema:
		return `Add "format": "date-time" (RFC 3339, with Z or an offset), or "format": "date" ` +
			"for a calendar date, and enable format assertion in the validator."
	case SourceGo:
		return "Use time.Time (or civil.Date for a calendar date) instead of a string."
	default:
		return fmt.Sprintf("Store it as date or timestamptz: ALTER TABLE %s ALTER COLUMN %s "+
			"TYPE timestamptz USING to_timestamp(%s, '<format>');", e.Name, f.Name, f.Name)
	}
}

func rawOr(f *Field, fallback string) string {
	if f.RawType != "" {
		return f.RawType
	}
	return fallback
}

// fieldAnnotationHint writes a field-level annotation in the syntax of e's
// source.
func fieldAnnotationHint(e *Entity, f *Field, key, value string) string {
	switch e.Source {
	case SourceGo:
		return fmt.Sprintf("`mml:\"%s=%s\"` on %s", key, value, f.Name)
	case SourceJSONSchema:
		return fmt.Sprintf(`"x-mml-%s": %q in property %s`, key, value, f.Name)
	default:
		return fmt.Sprintf("COMMENT ON COLUMN %s.%s IS '@%s: %s'", e.Name, f.Name, key, value)
	}
}
