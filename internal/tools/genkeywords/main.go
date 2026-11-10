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
	"cmp"
	"context"
	"encoding/json"
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

// pgquery is the module that compiles the parser the linter runs.
const pgquery = "github.com/wasilibs/go-pgquery"

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
	dir, err := pgqueryDir(ctx)
	if err != nil {
		return err
	}
	src, err := os.ReadFile(filepath.Join(dir, kwlist))
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

// pgqueryDir returns the directory of go-pgquery at the version go.mod
// requires, downloading it first. go list -m prints an empty Dir for a
// module not yet in the module cache, as on a fresh CI runner.
func pgqueryDir(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "go", "mod", "download", "-json", pgquery).Output()
	if err != nil {
		return "", fmt.Errorf("download %s: %w", pgquery, err)
	}
	var mod struct {
		Dir   string `json:"Dir"`
		Error string `json:"Error"`
	}
	if err := json.Unmarshal(out, &mod); err != nil {
		return "", fmt.Errorf("decode go mod download output: %w", err)
	}
	if mod.Error != "" || mod.Dir == "" {
		return "", fmt.Errorf("download %s: %s", pgquery, cmp.Or(mod.Error, "no directory"))
	}
	return mod.Dir, nil
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
