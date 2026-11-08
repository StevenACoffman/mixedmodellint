// Command genvaluesets writes the code lists §2.7-B compares local enums
// with, one code per line, sorted: ISO 3166-1 alpha-2 country codes, ISO 4217
// currency codes and ISO 639-1 language codes, plus the country aliases
// CLDR replaces with an ISO code. The codes come from
// golang.org/x/text, which tracks the CLDR and IANA registries, so the lists
// are generated rather than copied (rule_plan.md §2.7-B, "bundled value
// sets").
//
//	go run ./internal/tools/genvaluesets -o internal/mixedmodellint/lexicon
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/text/currency"
	"golang.org/x/text/language"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "genvaluesets:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("genvaluesets", flag.ContinueOnError)
	out := fs.String("o", "", "directory to write the lists to")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}
	if *out == "" {
		return errors.New("-o is required")
	}
	lists := map[string][]string{
		"iso3166_alpha2.txt":  codes(2, isCountry),
		"iso4217.txt":         codes(3, isCurrency),
		"iso639_1.txt":        codes(2, isLanguage),
		"iso3166_aliases.txt": countryAliases(),
	}
	for name, list := range lists {
		if len(list) == 0 {
			return fmt.Errorf("%s is empty; has golang.org/x/text changed?", name)
		}
		text := strings.Join(list, "\n") + "\n"
		if err := os.WriteFile(filepath.Join(*out, name), []byte(text), 0o600); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
	}
	return nil
}

// codes returns every upper-case code of n letters that keep accepts, in
// alphabetical order.
func codes(n int, keep func(string) bool) []string {
	var out []string
	var walk func(prefix string)
	walk = func(prefix string) {
		if len(prefix) == n {
			if keep(prefix) {
				out = append(out, prefix)
			}
			return
		}
		for r := 'A'; r <= 'Z'; r++ {
			walk(prefix + string(r))
		}
	}
	walk("")
	return out
}

// isCountry accepts an ISO 3166-1 alpha-2 code. ParseRegion also accepts
// codes ISO does not assign to a country, each left out by one test:
// aliases such as UK and BU canonicalize to another code (and are what
// §2.7-B reports as off-standard); private-use codes such as XK and ZZ;
// and reservations such as EU, AC and IC, which have no UN M49 number or ISO
// alpha-3 code. Codes ISO has withdrawn but CLDR still knows (YU, SU) remain.
func isCountry(code string) bool {
	r, err := language.ParseRegion(code)
	return err == nil && r.String() == code && r.Canonicalize() == r && r.IsCountry() &&
		!r.IsPrivateUse() && r.M49() != 0 && r.ISO3() != "ZZZ"
}

// countryAliases returns "ALIAS CODE" for each two-letter region code that
// CLDR replaces with an ISO 3166-1 code, such as "UK GB": the off-standard
// variants §2.7-B names in its findings.
func countryAliases() []string {
	var out []string
	for _, alias := range codes(2, func(string) bool { return true }) {
		r, err := language.ParseRegion(alias)
		if err != nil || r.IsPrivateUse() {
			continue
		}
		if canon := r.Canonicalize().String(); canon != alias && isCountry(canon) {
			out = append(out, alias+" "+canon)
		}
	}
	return out
}

func isCurrency(code string) bool {
	u, err := currency.ParseISO(code)
	return err == nil && u.String() == code
}

// isLanguage accepts a two-letter ISO 639-1 code. ParseBase also accepts
// deprecated codes that canonicalize to another (iw to he); those are left
// out for the same reason as country aliases.
func isLanguage(code string) bool {
	lower := strings.ToLower(code)
	b, err := language.ParseBase(lower)
	return err == nil && b.String() == lower
}
