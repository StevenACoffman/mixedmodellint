package mixedmodellint

import (
	_ "embed"
	"regexp"
	"strings"
)

// The code lists §2.7 compares local values with are generated from
// golang.org/x/text by internal/tools/genvaluesets.
//
//go:generate go run ../tools/genvaluesets -o lexicon
var (
	//go:embed lexicon/iso3166_alpha2.txt
	iso3166Codes string
	//go:embed lexicon/iso3166_aliases.txt
	iso3166Aliases string
	//go:embed lexicon/iso4217.txt
	iso4217Codes string
	//go:embed lexicon/iso639_1.txt
	iso639Codes string
)

// meaningLexicon holds the vocabulary of §2, shared meaning: which names
// classify, which measure, and the standard vocabularies values can follow.
type meaningLexicon struct {
	classification map[string]bool    // 2.3-A: words naming a classification
	measureName    *regexp.Regexp     // 2.5-A: numeric fields that need a unit
	valueSets      []valueSet         // 2.7-B: bundled standard code lists
	regulated      []regulatedConcept // 2.7-A: concepts with a standard vocabulary
}

// valueSet is a standard code list that can be bundled (§2.7-B). Codes are
// held in upper case and compared case-insensitively. aliases maps an
// off-standard code people use to the standard one (UK to GB). nameHint
// breaks a tie between two lists a value set matches equally (FR is both a
// country and a language) by the field's name.
type valueSet struct {
	standard string
	codes    map[string]bool
	aliases  map[string]string
	nameHint *regexp.Regexp
}

// regulatedConcept is a concept with a standard vocabulary that a field
// recognizably holds (§2.7-A). doc matches a description that names the
// concept's standard or its domain term, the lexical second signal; values
// is the bundled value set of the concept, when it has one, for the
// structural second signal.
type regulatedConcept struct {
	concept  string
	standard string
	name     *regexp.Regexp
	doc      *regexp.Regexp
	values   string
}

func newMeaningLexicon() meaningLexicon {
	return meaningLexicon{
		classification: wordSet("category region area territory segment tier class " +
			"classification genre department geo geography"),
		measureName: regexp.MustCompile(`(?i)(amount|price|cost|revenue|weight|length|` +
			`height|distance|duration|qty|quantity|total|fee|balance)`),
		valueSets: standardValueSets(),
		regulated: regulatedConcepts(),
	}
}

func standardValueSets() []valueSet {
	return []valueSet{
		{
			standard: "ISO 3166-1 alpha-2", codes: upperSet(iso3166Codes),
			aliases:  aliasMap(iso3166Aliases),
			nameHint: regexp.MustCompile(`(?i)(country|nation|iso2)`),
		},
		{
			standard: "ISO 4217", codes: upperSet(iso4217Codes),
			nameHint: regexp.MustCompile(`(?i)(currency|ccy)`),
		},
		{
			standard: "ISO 639-1", codes: upperSet(iso639Codes),
			nameHint: regexp.MustCompile(`(?i)(lang|locale)`),
		},
	}
}

// regulatedConcepts is rule_plan.md §2.7-A's table of concepts with a
// standard vocabulary, in the order a name is tried against them.
func regulatedConcepts() []regulatedConcept {
	concept := func(c, standard, name, doc, values string) regulatedConcept {
		return regulatedConcept{
			concept: c, standard: standard, values: values,
			name: regexp.MustCompile(`(?i)(^|_)(` + name + `)`),
			doc:  regexp.MustCompile(`(?i)\b(` + doc + `)`),
		}
	}
	return []regulatedConcept{
		concept("diagnosis", "ICD-10 or SNOMED CT", "diagnos|icd", "icd|snomed|diagnosis code", ""),
		concept("procedure", "CPT or ICD-10-PCS", "procedure", "cpt|icd-10-pcs|procedure code", ""),
		concept("medication", "RxNorm", "drug|medication", "rxnorm|ndc", ""),
		concept("country", "ISO 3166-1", "country", "iso 3166|iso country",
			"ISO 3166-1 alpha-2"),
		concept("currency", "ISO 4217", "currency", "iso 4217|iso currency", "ISO 4217"),
		concept("language", "BCP 47", "language|locale", "bcp 47|bcp47|iso 639", "ISO 639-1"),
		concept("unit of measure", "UCUM", "unit_of_measure|uom", "ucum", ""),
		concept("industry", "NAICS", "industry|naics", "naics|sic code", ""),
		concept("sex or gender", "ISO/IEC 5218 or HL7 AdministrativeGender", "sex(_|$)|gender",
			"iso/iec 5218|5218|hl7", ""),
	}
}

// upperSet returns the codes of a generated list, one per line, upper-cased.
func upperSet(list string) map[string]bool {
	out := map[string]bool{}
	for w := range wordSet(list) {
		out[strings.ToUpper(w)] = true
	}
	return out
}

// aliasMap reads "ALIAS CODE" lines.
func aliasMap(list string) map[string]string {
	out := map[string]string{}
	for line := range strings.Lines(list) {
		if alias, code, ok := strings.Cut(strings.TrimSpace(line), " "); ok {
			out[alias] = code
		}
	}
	return out
}
