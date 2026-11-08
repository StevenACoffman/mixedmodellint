package mixedmodellint

import (
	_ "embed" // the §5.1-A keyword lists
	"regexp"
	"strings"
)

const (
	nameOK nameClass = iota
	nameReserved
	nameAmbiguousType
	nameWeaklyAmbiguous
	nameGeneric
	nameSQLReserved
)

// postgresKeywords is generated from the parser's kwlist.h by
// internal/tools/genkeywords: every keyword a column name cannot use
// unquoted. sql2016Keywords is the SQL:2016 column of PostgreSQL 17's
// keyword appendix (Appendix C), the words the standard reserves.
// eventVerbs is curated by hand for §5.1-B; its header says what it holds.
//
//go:generate go run ../tools/genkeywords -o lexicon/postgres_keywords.txt
var (
	//go:embed lexicon/postgres_keywords.txt
	postgresKeywords string
	//go:embed lexicon/sql2016_reserved.txt
	sql2016Keywords string
	//go:embed lexicon/event_verbs.txt
	eventVerbs string
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
	periodGrain  *regexp.Regexp // 8.5-B: grain text naming a calendar period
	windowSpec   *regexp.Regexp // 8.6-A: a window= declaration

	eventName      *regexp.Regexp // 9.1-A: entity names of event and fact tables
	fileLocation   *regexp.Regexp // 9.4-A: where a file is stored
	fileDescriptor *regexp.Regexp // 9.4-A: what a file is
	fileCreated    *regexp.Regexp // 9.4-A: when a file was made
	fileIngested   *regexp.Regexp // 9.4-A: when a file arrived
	contentHash    *regexp.Regexp // 9.4-A: a hash of a file's bytes
	fileName       *regexp.Regexp // 9.4-B: a file's name, never a dedup key

	scratchName           *regexp.Regexp // 1.7-A: names of one-off models
	principal             *regexp.Regexp // 1.2-A: who may own or settle disputes
	placeholderDefinition *regexp.Regexp // 1.2-A: definitions that say nothing

	mutableKey    *regexp.Regexp // 4.1-A: natural keys whose values change
	timeKey       *regexp.Regexp // 4.1-A: keys that are times
	sourceKey     *regexp.Regexp // 4.2-A: another system's ID for the same thing
	derivedName   *regexp.Regexp // 4.6-A: names of things cut out of a file
	locationField *regexp.Regexp // 4.6-A: where in the source a derived thing is
	fileRefField  *regexp.Regexp // 4.6-A: a file referenced by name, path or URL

	localName          *regexp.Regexp // 9.2-B: wall-clock times
	zoneName           *regexp.Regexp // 9.2-B: fields holding a time zone
	ianaZone           *regexp.Regexp // 9.2-B: a zone name, not an offset or abbreviation
	timestampPrecision *regexp.Regexp // 9.8-A: timestamp(p) and timestamptz(p)
	quantityField      *regexp.Regexp // 3.4-A: amounts that must be positive
	masterName         *regexp.Regexp // 3.4-A: names of likely master data
	widerToken         *regexp.Regexp // 7.4-A: words for values wider than a row

	postgresReserved map[string]bool // 5.1-A list 1
	sql2016Reserved  map[string]bool // 5.1-A list 2
	eventWords       map[string]bool // 5.1-B: verbs, participles and event nouns

	meaningLexicon
	agentLexicon
	contentLexicon
	shapeLexicon
}

func newLexicon() *lexicon {
	return &lexicon{
		postgresReserved: wordSet(postgresKeywords),
		sql2016Reserved:  wordSet(sql2016Keywords),
		eventWords:       wordSet(eventVerbs),
		meaningLexicon:   newMeaningLexicon(),
		agentLexicon:     newAgentLexicon(),
		contentLexicon:   newContentLexicon(),
		shapeLexicon:     newShapeLexicon(),
		numbered: regexp.MustCompile(
			`^(?P<stem>[a-z][a-z_]*?)_?(?P<n>\d+)(?P<suffix>_[a-z]+)?$`,
		),
		grainPattern: regexp.MustCompile(
			`(?i)^one row per [a-z0-9_ ]+?( per [a-z0-9_ ]+?)*( \([^)]*\))?$`),
		mixedField: regexp.MustCompile(`(?i)^(grain_level|period_type|granularity|agg_level)$`),
		aggCounter: regexp.MustCompile(`(?i)^(count|total|sum)_|_(count|total)$`),
		duration: regexp.MustCompile(
			`(?i)(duration|elapsed|latency|response_time|_ms|_secs?|_seconds)$`),
		eventName: regexp.MustCompile(`(?i)(event|log|click|txn|transaction|^fact_|^fct_)`),
		fileLocation: regexp.MustCompile(
			`(?i)(_uri$|_url$|^s3_|^gcs_|object_key$|storage_path$|file_path$)`),
		fileDescriptor: regexp.MustCompile(
			`(?i)(filename|file_name|mime|content_type|size_bytes|byte_size|sha256|checksum)`),
		fileCreated: regexp.MustCompile(
			`(?i)^(created|captured|taken|exif|recorded|authored|document_date)(_?(at|time))?`),
		fileIngested: regexp.MustCompile(`(?i)^(ingested_at|uploaded_at|received_at)$`),
		contentHash:  regexp.MustCompile(`(?i)(sha256|sha1|md5|content_hash|checksum)`),
		fileName:     regexp.MustCompile(`(?i)^(filename|file_name|original_name)$`),
		scratchName: regexp.MustCompile(`(?i)(^tmp_|^test_|_v\d+$|_old$|_new$|_copy$|_bak$|` +
			`_(mon|tues?|wed|thur?s?|fri|sat|sun)(day)?(_|$)|_\d{8}$)`),
		principal: regexp.MustCompile(
			`^((team|group|role):[a-z0-9-]+|@[A-Za-z0-9_.-]+(/[A-Za-z0-9_.-]+)?)$`),
		mutableKey: regexp.MustCompile(`(?i)(^|_)(e?mail|phone|mobile|msisdn|name|first_name|` +
			`last_name|full_name|username|handle|ssn|address)$`),
		timeKey: regexp.MustCompile(`(?i)(^|_)(start(ed)?_at|created_at|timestamp)$`),
		sourceKey: regexp.MustCompile(`(?i)^(legacy_|src_|source_)?(crm|erp|stripe|salesforce|` +
			`hubspot|netsuite|shopify|zendesk)_(id|key)$`),
		derivedName: regexp.MustCompile(`(?i)(detection|segment|chunk|span|frame|annotation|` +
			`transcript_line|bounding_box|excerpt|passage)s?$`),
		locationField: regexp.MustCompile(`(?i)^((ts|time|offset)_(start|end)|start_ms|end_ms|` +
			`bbox|x|y|width|height|polygon|(char|byte|token)_(start|end|offset)|page|line|` +
			`chunk_index|sequence)$`),
		fileRefField:       regexp.MustCompile(`(?i)_(filename|path|url)$`),
		timestampPrecision: regexp.MustCompile(`^timestamp(?:tz)?\((\d+)\)`),
		localName:          regexp.MustCompile(`(?i)(^|_)local_`),
		zoneName:           regexp.MustCompile(`(?i)(_tz|timezone|time_zone|_zone)$`),
		ianaZone:           regexp.MustCompile(`^(UTC|[A-Za-z]+(/[A-Za-z0-9_+-]+)+)$`),
		quantityField:      regexp.MustCompile(`(_qty|_quantity|_weight|_length)$`),

		widerToken: regexp.MustCompile(`(^|_)(lifetime|header|overall)(_|$)`),
		masterName: regexp.MustCompile(
			`(?i)(item|sku|product|location|unit|currency|supplier)`),
		placeholderDefinition: regexp.MustCompile(
			`(?i)^(todo|tbd|n/?a|the \w+ (table|struct|schema))\b`),
		periodGrain: regexp.MustCompile(`(?i)\bper (day|week|month|quarter|year)\b`),
		windowSpec: regexp.MustCompile(
			`^(tumbling\([^,()]+\)|sliding\([^,()]+,\s*[^,()]+\)|session\([^,()]+\))$`),
		coarseGrain: regexp.MustCompile(
			`(?i)\bper (day|week|month|quarter|year|hour|store|region|country|segment|cohort|` +
				`category|product|customer|campaign|channel)\b`),
	}
}

// classifyName applies §5.1-A's lists 1, 2, 4, 5 and 6 to a lowercase name:
// PostgreSQL's reserved words, then the bare type words, the weakly
// ambiguous and the generic words, and last SQL:2016's reserved words, so a
// name that is generic and also SQL:2016-reserved (value, count) is graded
// as generic. status and state are deliberately not listed: the revised
// rule's own passing example has a bare status column.
func (rx *lexicon) classifyName(name string) nameClass {
	if rx.postgresReserved[name] {
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
	}
	if rx.sql2016Reserved[name] {
		return nameSQLReserved
	}
	return nameOK
}

// timeUnitRank orders time units from finest to coarsest, for comparing the
// grain a query asks for with the grain a source has (7.5-D). Unknown units
// rank 0.
func timeUnitRank(unit string) int {
	ranks := map[string]int{
		"nanosecond": 1, "nanoseconds": 1,
		"microsecond": 2, "microseconds": 2,
		"millisecond": 3, "milliseconds": 3,
		"second": 4, "seconds": 4, "epoch": 4,
		"minute": 5, "minutes": 5,
		"hour": 6, "hours": 6,
		"day": 7, "days": 7, "dow": 7, "doy": 7, "isodow": 7, "date": 7,
		"week": 8, "weeks": 8,
		"month": 9, "months": 9,
		"quarter": 10,
		"year":    11, "years": 11, "decade": 11, "century": 11, "millennium": 11,
	}
	return ranks[strings.ToLower(strings.TrimSpace(unit))]
}

// splitList splits an annotation value that lists names, separated by
// commas, spaces or both: "captured_at, ingested_at".
func splitList(v string) []string {
	return strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' })
}

// wordSet returns the words of a list, one or more per line, skipping
// lines that begin with # so a hand-curated list can say where it comes from.
func wordSet(list string) map[string]bool {
	out := map[string]bool{}
	for line := range strings.Lines(list) {
		if strings.HasPrefix(line, "#") {
			continue
		}
		for _, w := range strings.Fields(line) {
			out[w] = true
		}
	}
	return out
}
