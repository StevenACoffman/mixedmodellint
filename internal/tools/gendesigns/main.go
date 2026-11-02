// Command gendesigns extracts every design block of rule_plan.md, with the
// heading of the rule it belongs to, into the text the explain command
// embeds. rule_plan.md stays the one authority; the text is derived from it,
// and `just generate-check` fails when the two drift apart.
//
//	go run ./internal/tools/gendesigns -i rule_plan.md -o cmd/explain/designs.txt
//
// Each block starts with a line "=== <design ID>", such as "=== 7.1-B".
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "gendesigns:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("gendesigns", flag.ContinueOnError)
	in := fs.String("i", "rule_plan.md", "rule plan to read")
	out := fs.String("o", "", "file to write")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}
	if *out == "" {
		return errors.New("-o is required")
	}
	src, err := os.ReadFile(*in)
	if err != nil {
		return fmt.Errorf("read %s: %w", *in, err)
	}
	text := extract(string(src))
	if text == "" {
		return errors.New("no design blocks found; has the rule plan's format changed?")
	}
	if err := os.WriteFile(*out, []byte(text), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", *out, err)
	}
	return nil
}

// extract returns the design blocks of a rule plan. A block runs from its
// "#### Design X — title" heading to the next heading or recommendation, and
// is preceded by its rule's "### §N.M — title" heading.
func extract(src string) string {
	ruleRx := regexp.MustCompile(`^### §(\d+\.\d+) — (.*)$`)
	designRx := regexp.MustCompile(`^#### Design ([A-Z]) — (.*)$`)
	var b strings.Builder
	rule, ruleTitle := "", ""
	inDesign := false
	for _, line := range strings.Split(src, "\n") {
		switch {
		case ruleRx.MatchString(line):
			m := ruleRx.FindStringSubmatch(line)
			rule, ruleTitle, inDesign = m[1], m[2], false
		case designRx.MatchString(line) && rule != "":
			m := designRx.FindStringSubmatch(line)
			fmt.Fprintf(&b, "=== %s-%s\n§%s — %s\nDesign %s — %s\n", rule, m[1], rule, ruleTitle,
				m[1], m[2])
			inDesign = true
		case strings.HasPrefix(line, "#") || strings.HasPrefix(line, "**Recommendation"):
			inDesign = false
		case inDesign:
			b.WriteString(line + "\n")
		}
	}
	return b.String()
}
