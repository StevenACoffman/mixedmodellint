package mixedmodellint

import (
	"regexp"
	"strings"
)

const (
	nameOK nameClass = iota
	nameReserved
	nameAmbiguousType
	nameWeaklyAmbiguous
	nameGeneric
)

// nameClass is how §5.1 classifies a bare column name.
type nameClass int

// lexicon holds the compiled patterns the designs share. It is built once per
// Analyze call and handed to designs through the catalog, so there are no
// package-level regexps (summary_rules §1, "no global state").
type lexicon struct {
	numbered     *regexp.Regexp // 5.5-A: stem, number, optional suffix
	grainPattern *regexp.Regexp // 7.1-A: "one row per X( per Y)*( (qualifier))?"
	mixedField   *regexp.Regexp // 7.4-B: discriminator names
	aggCounter   *regexp.Regexp // 7.5-C: count_/total_/sum_ prefixes and _count/_total suffixes
	duration     *regexp.Regexp // 9.2-A: names that are durations, not instants
	coarseGrain  *regexp.Regexp // 8.4-A: grain text coarser than atomic
}

func newLexicon() *lexicon {
	return &lexicon{
		numbered: regexp.MustCompile(`^(?P<stem>[a-z][a-z_]*?)_?(?P<n>\d+)(?P<suffix>_[a-z]+)?$`),
		grainPattern: regexp.MustCompile(
			`(?i)^one row per [a-z0-9_ ]+?( per [a-z0-9_ ]+?)*( \([^)]*\))?$`),
		mixedField: regexp.MustCompile(`(?i)^(grain_level|period_type|granularity|agg_level)$`),
		aggCounter: regexp.MustCompile(`(?i)^(count|total|sum)_|_(count|total)$`),
		duration: regexp.MustCompile(
			`(?i)(duration|elapsed|latency|response_time|_ms|_secs?|_seconds)$`),
		coarseGrain: regexp.MustCompile(
			`(?i)\bper (day|week|month|quarter|year|hour|store|region|country|segment|cohort|` +
				`category|product|customer|campaign|channel)\b`),
	}
}

// postgresReserved reports whether w is a PostgreSQL 17 keyword of category
// RESERVED_KEYWORD or TYPE_FUNC_NAME_KEYWORD, the two that cannot be a column
// name without quoting. Transcribed from src/include/parser/kwlist.h; it must
// be re-checked for each PostgreSQL major version.
func postgresReserved(w string) bool {
	switch w {
	case "all", "analyse", "analyze", "and", "any", "array", "as", "asc", "asymmetric",
		"both", "case", "cast", "check", "collate", "column", "constraint", "create",
		"current_catalog", "current_date", "current_role", "current_time",
		"current_timestamp", "current_user", "default", "deferrable", "desc", "distinct",
		"do", "else", "end", "except", "false", "fetch", "for", "foreign", "from", "grant",
		"group", "having", "in", "initially", "intersect", "into", "lateral", "leading",
		"limit", "localtime", "localtimestamp", "not", "null", "offset", "on", "only",
		"or", "order", "placing", "primary", "references", "returning", "select",
		"session_user", "some", "symmetric", "system_user", "table", "then", "to",
		"trailing", "true", "union", "unique", "user", "using", "variadic", "when",
		"where", "window", "with",
		// TYPE_FUNC_NAME_KEYWORD
		"authorization", "binary", "collation", "concurrently", "cross", "current_schema",
		"freeze", "full", "ilike", "inner", "is", "isnull", "join", "left", "like",
		"natural", "notnull", "outer", "overlaps", "right", "similar", "tablesample",
		"verbose":
		return true
	default:
		return false
	}
}

// classifyName applies §5.1-A's lists 1, 4, 5 and 6 to a lowercase name.
// status and state are deliberately not listed: the revised rule's own
// passing example has a bare status column.
func classifyName(name string) nameClass {
	if postgresReserved(name) {
		return nameReserved
	}
	switch name {
	case "date", "time", "timestamp", "datetime", "interval", "boolean", "json", "text", "number":
		return nameAmbiguousType
	case "type", "kind":
		return nameWeaklyAmbiguous
	case "name", "value", "data", "info", "flag", "amount", "category", "code", "key",
		"level", "count", "description":
		return nameGeneric
	default:
		return nameOK
	}
}

// timeUnitRank orders time units from finest to coarsest, for comparing the
// grain a query asks for with the grain a source has (7.5-D). Unknown units
// rank 0.
func timeUnitRank(unit string) int {
	ranks := map[string]int{
		"microsecond": 1, "microseconds": 1, "millisecond": 1, "milliseconds": 1,
		"second": 2, "seconds": 2, "epoch": 2,
		"minute": 3, "minutes": 3,
		"hour": 4, "hours": 4,
		"day": 5, "days": 5, "dow": 5, "doy": 5, "isodow": 5, "date": 5,
		"week": 6, "weeks": 6,
		"month": 7, "months": 7,
		"quarter": 8,
		"year":    9, "years": 9, "decade": 9, "century": 9, "millennium": 9,
	}
	return ranks[strings.ToLower(strings.TrimSpace(unit))]
}
