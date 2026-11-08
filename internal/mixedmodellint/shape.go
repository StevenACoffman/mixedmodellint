package mixedmodellint

import (
	_ "embed" // the §5.2-B abbreviation table
	"regexp"
	"strings"
)

// abbreviationList is curated by hand for §5.2-B; its header says what it
// holds.
//
//go:embed lexicon/abbreviations.txt
var abbreviationList string

// patternCluster is a group of attributes that a published data model
// pattern factors out (§4.11): an entity covering most of its slots repeats
// the pattern by hand. Each slot lists the field stems that fill it.
type patternCluster struct {
	name    string
	pattern string   // the pattern to evaluate, as pattern= names it
	shared  []string // entities that hold the cluster once for everyone
	slots   [][]string
}

// shapeLexicon holds the patterns of the designs about an entity's shape and
// keys and the records that justify them: §1.9, §3, §4, §5, §6, §7.6 and §8.
type shapeLexicon struct {
	shortcut     *regexp.Regexp    // 1.9-B: markers of a modeling shortcut
	adrRationale *regexp.Regexp    // 1.9-B: an ADR's rationale heading
	adrCost      *regexp.Regexp    // 1.9-B: an ADR's cost heading
	emptySection *regexp.Regexp    // 1.9-B: section text that records nothing
	obtName      *regexp.Regexp    // 3.3-B: names of One Big Tables
	lineOrdinal  *regexp.Regexp    // 4.4-A: key columns that number rows within a parent
	surrogate    *regexp.Regexp    // 4.4-A: defaults the database generates keys with
	naturalID    *regexp.Regexp    // 4.4-B: natural and legacy identifiers
	displayRef   *regexp.Regexp    // 4.5-A: a display value of another entity
	roleDisplay  *regexp.Regexp    // 4.5-A: a display value of a person in a role
	hashName     *regexp.Regexp    // 4.7-A: content-derived values
	snapshotName *regexp.Regexp    // 4.8-B: a value captured at an event
	junctionName *regexp.Regexp    // 4.9-A: names of link tables
	genericNoun  map[string]bool   // 4.10-A: trailing nouns that do not make a concept
	genericField map[string]bool   // 4.10-A: fields any two lookup tables share
	clusters     []patternCluster  // 4.11-A: attribute groups patterns factor out
	patterns     map[string]bool   // 4.11-B: pattern names a pattern= record may use
	abbrev       map[string]string // 5.2-B: cryptic abbreviations and their words
	crypticName  *regexp.Regexp    // 5.2-B: a few letters and a number
	categorical  *regexp.Regexp    // 5.4-A: names of coded values
	satellite    map[string]bool   // 6.4-A: suffixes of a relationship's attributes
	whenSuffix   map[string]bool   // 6.4-A: of those, the ones that hold a date
	growth       map[string]bool   // 6.4-C: suffixes that turn a reference into a relationship
	growingList  *regexp.Regexp    // 6.5-B: names of lists that only grow
	volatile     *regexp.Regexp    // 6.6-A: master values that change over time
	lineName     *regexp.Regexp    // 6.6-A: names of line items, for an entity with no key
	validity     *regexp.Regexp    // 6.6-A: columns that bound when a value held
	inferredName *regexp.Regexp    // 6.7-A: names of inferred-link entities
	scoreName    *regexp.Regexp    // 6.7-A: a score of an inference
	confidence   *regexp.Regexp    // 6.7-A: how sure an inference is
	method       *regexp.Regexp    // 6.7-A: how an inference was made
	decision     *regexp.Regexp    // 6.7-A: what review decided
	reviewState  *regexp.Regexp    // 6.7-A: a value meaning a person must look
	chunkName    *regexp.Regexp    // 7.6-A: names of RAG chunk entities
	chunkSize    *regexp.Regexp    // 7.6-A: a grain stating a chunk's size
}

func newShapeLexicon() shapeLexicon {
	return shapeLexicon{
		shortcut: regexp.MustCompile(`(?i)\b(for now|temporary|temp hack|hack|TODO|FIXME|XXX|` +
			`quick fix|until we)\b`),
		adrRationale: regexp.MustCompile(`(?i)^#+\s*rationale\b`),
		adrCost:      regexp.MustCompile(`(?i)^#+\s*(costs?|consequences)\b`),
		emptySection: regexp.MustCompile(`(?i)^\s*(todo|tbd|n/?a|-|\.{3})?\s*$`),
		obtName:      regexp.MustCompile(`(?i)^(obt|wide)_`),
		lineOrdinal: regexp.MustCompile(`(?i)^(line_?(no|num|number)|seq|sequence|position|` +
			`ordinal|valid_from|version)$`),
		surrogate: regexp.MustCompile(`(?i)(nextval|gen_random_uuid|uuid_generate|identity)`),
		naturalID: regexp.MustCompile(`(?i)^(legacy_\w+_id|\w+_(external|source|erp|crm)_id|` +
			`(erp|crm)_id|email|sku|isbn|ein|vat_id|tax_id)$`),
		displayRef: regexp.MustCompile(`(?i)^(\w+?)_(display_name|name|filename|title|` +
			`email|handle|username)$`),
		roleDisplay:  regexp.MustCompile(`(?i)^(reviewer|author|uploader|owner)_(name|email)$`),
		snapshotName: regexp.MustCompile(`(?i)_at_[a-z]+$`),
		junctionName: regexp.MustCompile(`(?i)(_xref|_map|_link|_rel|_join|_assoc|_bridge)$`),
		genericNoun:  wordSet("profile account record info detail data"),
		genericField: wordSet("code label name description sort_order position title " +
			"is_active notes"),
		clusters:   patternClusters(),
		abbrev:     pairs(abbreviationList),
		satellite:  wordSet("since from to on date start end role terms location notes"),
		whenSuffix: wordSet("since from to on date start end"),
		growth:     wordSet("since until role terms"),
		growingList: regexp.MustCompile(
			`(?i)(history|events|log|orders|messages|transactions|activity)`,
		),
		volatile: regexp.MustCompile(`(?i)(price|cost|rate|fee|discount)`),
		lineName: regexp.MustCompile(`(?i)(_items?|_lines?|_line_items?|LineItem|Item)$`),
		validity: regexp.MustCompile(`(?i)^(valid|effective)_(from|to|until)$`),
		inferredName: regexp.MustCompile(`(?i)(match|similar|recommend|candidate|dedup|` +
			`link_suggest|resolution)`),
		scoreName:  regexp.MustCompile(`(?i)(score|similarity|cosine|distance|probability)`),
		confidence: regexp.MustCompile(`(?i)^(confidence|score|probability|similarity)$`),
		method: regexp.MustCompile(
			`(?i)^(match_method|method|model_version|model|algorithm)$`,
		),
		decision:    regexp.MustCompile(`(?i)^(status|review_status|decision|review_state)$`),
		reviewState: regexp.MustCompile(`(?i)(pending|review)`),
		chunkName:   regexp.MustCompile(`(?i)(chunk|embedding|document_segment)`),
		chunkSize:   regexp.MustCompile(`(?i)\d+[- ]?(token|char|character|word)s?\b`),
		crypticName: regexp.MustCompile(`^[A-Za-z]{1,4}_?\d+$`),
		categorical: regexp.MustCompile(`(?i)(^|_)(status|state|type|kind|category|reason|` +
			`code|level|tier|channel|priority|stage)$`),
		patterns: wordSet("account agreement event location party_role product classification"),
		hashName: regexp.MustCompile(`(?i)(^|_)(sha(1|256|512)?|md5|hash|digest|checksum|` +
			`etag)($|_)`),
	}
}

// patternClusters returns the clusters of rule_plan.md §4.11-A.
func patternClusters() []patternCluster {
	return []patternCluster{
		{
			name: "party contact", pattern: "party_role",
			shared: []string{"party", "contact", "person", "contact_method"},
			slots: [][]string{
				{"name", "full_name", "first_name", "legal_name"},
				{"email"},
				{"phone", "phone_number"},
				{"address_line1", "street", "address"},
				{"city"},
				{"postal_code", "zip", "zip_code"},
				{"country", "country_code"},
			},
		},
		{
			name: "location", pattern: "location", shared: []string{"location", "address"},
			slots: [][]string{
				{"address_line1", "street"},
				{"city"},
				{"region", "state"},
				{"postal_code", "zip", "zip_code"},
				{"country", "country_code"},
				{"latitude"},
				{"longitude"},
			},
		},
		{
			name: "agreement", pattern: "agreement", shared: []string{"agreement", "contract"},
			slots: [][]string{
				{"start_date", "effective_from"},
				{"end_date", "effective_to"},
				{"terms"},
				{"signed_at"},
			},
		},
		{
			name: "account", pattern: "account", shared: []string{"account"},
			slots: [][]string{{"account_number"}, {"opened_at"}, {"closed_at"}, {"balance"}},
		},
	}
}

// pairs reads a list of "key value" lines, skipping # comments.
func pairs(list string) map[string]string {
	out := map[string]string{}
	for line := range strings.Lines(list) {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), " "); ok && !strings.HasPrefix(k, "#") {
			out[k] = strings.TrimSpace(v)
		}
	}
	return out
}
