// Command genkeywords writes the PostgreSQL keywords that cannot be column
// names without quoting, one per line, sorted: the RESERVED_KEYWORD and
// TYPE_FUNC_NAME_KEYWORD entries of kwlist.h in the libpg_query that
// github.com/wasilibs/go-pgquery compiles, which is the parser the linter
// runs. rule_plan.md §5.1-A asks for the list to be generated, not copied,
// so it follows the parser through upgrades.
//
//	go run ./internal/tools/genkeywords -o internal/mixedmodellint/lexicon/postgres_keywords.txt
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// kwlist is where go-pgquery keeps PostgreSQL's keyword table.
const kwlist = "internal/cparser/include/postgres/parser/kwlist.h"

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "genkeywords:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("genkeywords", flag.ContinueOnError)
	out := fs.String("o", "", "file to write")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}
	if *out == "" {
		return errors.New("-o is required")
	}
	dir, err := exec.CommandContext(ctx, "go", "list", "-m", "-f", "{{.Dir}}",
		"github.com/wasilibs/go-pgquery").Output()
	if err != nil {
		return fmt.Errorf("find go-pgquery: %w", err)
	}
	src, err := os.ReadFile(filepath.Join(strings.TrimSpace(string(dir)), kwlist))
	if err != nil {
		return fmt.Errorf("read kwlist.h: %w", err)
	}
	words := reserved(src)
	if len(words) == 0 {
		return errors.New("kwlist.h has no reserved keywords; has its format changed?")
	}
	if err := os.WriteFile(*out, []byte(strings.Join(words, "\n")+"\n"), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", *out, err)
	}
	return nil
}

// reserved returns the keywords of the two categories a column name cannot
// use unquoted, sorted.
func reserved(src []byte) []string {
	rx := regexp.MustCompile(
		`PG_KEYWORD\("([a-z_]+)",\s*\w+,\s*(RESERVED_KEYWORD|TYPE_FUNC_NAME_KEYWORD)`,
	)
	var words []string
	for _, line := range bytes.Split(src, []byte("\n")) {
		if m := rx.FindSubmatch(line); m != nil {
			words = append(words, string(m[1]))
		}
	}
	slices.Sort(words)
	return slices.Compact(words)
}
