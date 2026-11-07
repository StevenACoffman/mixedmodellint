package lint_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/StevenACoffman/mixedmodellint/internal/lint"
	mml "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// TestGrainQueries runs the whole pipeline over a small repository whose
// queries each pass or fail one Query-family design, and checks which designs
// fire where.
func TestGrainQueries(t *testing.T) {
	t.Parallel()
	report, err := lint.Run(t.Context(), filepath.Join("testdata", "grain"), lint.Options{})
	if err != nil {
		t.Fatal(err)
	}
	// The fixture is about grain: §7, and §9.8-B, which 7.5-D builds on.
	var got []string
	for i := range report.Findings {
		f := &report.Findings[i]
		if strings.HasPrefix(f.Rule, "7.") || f.Design == "9.8-B" {
			got = append(got, fmt.Sprintf("%s:%d %s", f.Pos.File, f.Pos.Line, f.Design))
		}
	}
	want := []string{
		"app/app.go:15 7.2-A",
		"app/lines.go:47 7.4-D",
		"app/views.go:14 9.8-B",
		"db/002_lines.sql:5 7.4-A",
		"queries/fanout.sql:2 7.2-A",
		"queries/loads.sql:2 7.3-A",
		"queries/loads.sql:6 7.1-C",
		"queries/reads.sql:2 7.3-D",
		"queries/reads.sql:10 7.5-D",
		"queries/views.sql:2 7.5-D",
	}
	if !slices.Equal(want, got) {
		t.Fatalf("findings:\n got  %q\n want %q", got, want)
	}
}

// TestLockThenChange locks a model, changes it, and checks that the change
// is reported against the lock: the lock goes stale, a new table without a
// logical-model record fails §1.1-B, and one with an ADR passes.
func TestLockThenChange(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(name, src string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("db/001.sql", "-- mml:grain one row per order\n"+
		"CREATE TABLE orders (order_id BIGINT PRIMARY KEY);\n")
	l, err := lint.Lock(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := mml.WriteLock(&buf, l); err != nil {
		t.Fatal(err)
	}
	write(lint.LockName, buf.String())

	write("db/002.sql", "-- mml:grain one row per cart\n"+
		"CREATE TABLE carts (cart_id BIGINT PRIMARY KEY);\n"+
		"-- mml:grain one row per wishlist\n"+
		"CREATE TABLE wishlists (wishlist_id BIGINT PRIMARY KEY);\n")
	write("docs/adr/0001-wishlists.md", "# Add wishlists\n\nWe store a wishlist per shopper.\n")
	report, err := lint.Run(t.Context(), dir, lint.Options{})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for i := range report.Findings {
		if f := &report.Findings[i]; f.Design == "1.1-B" || f.Design == "mml/lock-stale" {
			got = append(got, f.Design+" "+f.Entity)
		}
	}
	slices.Sort(got)
	want := []string{"1.1-B carts", "mml/lock-stale carts", "mml/lock-stale wishlists"}
	if !slices.Equal(want, got) {
		t.Fatalf("findings:\n got  %q\n want %q", got, want)
	}
}

func TestBadLockIsAnError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, lint.LockName), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := lint.Run(t.Context(), dir, lint.Options{})
	if mml.ErrorCode(err) != mml.EINVALID {
		t.Fatalf("expected an EINVALID error, got %v", err)
	}
}

// TestBaseRef compares a change with its base revision, named by a git ref.
func TestBaseRef(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	ctx := t.Context()
	repo := t.TempDir()
	schema := filepath.Join(repo, "schema.sql")
	orders := "-- mml:grain one row per order\nCREATE TABLE orders (order_id BIGINT PRIMARY KEY);\n"
	carts := "-- mml:grain one row per cart\nCREATE TABLE carts (cart_id BIGINT PRIMARY KEY);\n"
	if err := os.WriteFile(schema, []byte(orders), 0o600); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, exec.CommandContext(ctx, "git", "init", "-q"))
	gitRun(t, repo, exec.CommandContext(ctx, "git", "add", "."))
	gitRun(t, repo, exec.CommandContext(ctx, "git", "commit", "-qm", "base"))
	if err := os.WriteFile(schema, []byte(orders+carts), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := lint.Run(ctx, repo, lint.Options{Base: "HEAD"})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for i := range report.Findings {
		if f := &report.Findings[i]; f.Design == "1.1-B" {
			got = append(got, f.Entity)
		}
	}
	if !slices.Equal([]string{"carts"}, got) {
		t.Fatalf("1.1-B reported %q, want only carts", got)
	}
	for _, ref := range []string{"missing", "HEAD\n--output=x"} {
		_, err := lint.Run(ctx, repo, lint.Options{Base: ref})
		if mml.ErrorCode(err) != mml.EINVALID {
			t.Errorf("base %q: got %v, want an EINVALID error", ref, err)
		}
	}
}

// gitRun runs a git command in dir, isolated from the user's git config.
func gitRun(t *testing.T, dir string, cmd *exec.Cmd) {
	t.Helper()
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s: %v\n%s", cmd, err, out)
	}
}

// TestReadsCodeowners checks that CODEOWNERS under .github, a hidden
// directory the walk otherwise skips, is read.
func TestReadsCodeowners(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		".github/CODEOWNERS": "* @acme/data\n",
		"schema.sql": "-- mml:grain one row per order\n" +
			"-- mml:definition A request from a customer to buy goods, placed at checkout and paid.\n" +
			"-- mml:owner @acme/orders\n-- mml:escalation role:cto\n" +
			"CREATE TABLE orders (order_id BIGINT PRIMARY KEY);\n",
	})
	report, err := lint.Run(t.Context(), dir, lint.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for i := range report.Findings {
		if f := &report.Findings[i]; f.Design == "1.2-A" &&
			strings.Contains(f.Measured, "not in CODEOWNERS") {
			return
		}
	}
	t.Fatal("an owner missing from .github/CODEOWNERS was not reported")
}

// TestFirstCodeownersWins checks that, as on GitHub, .github/CODEOWNERS is
// used and a root CODEOWNERS beside it is ignored.
func TestFirstCodeownersWins(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		files   map[string]string
		wantMsg string
	}{
		".github over the root": {
			files: map[string]string{
				".github/CODEOWNERS": "* @acme/data\n", "CODEOWNERS": "* @acme/orders\n",
			},
			wantMsg: "CODEOWNERS assigns it to @acme/data",
		},
		"the root over docs": {
			files: map[string]string{
				"CODEOWNERS": "/docs/ @acme/orders\n", "docs/CODEOWNERS": "* @acme/orders\n",
			},
			wantMsg: "no CODEOWNERS rule covers it",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			tc.files["schema.sql"] = "-- mml:owner team:orders\n" +
				"CREATE TABLE orders (order_id BIGINT PRIMARY KEY);\n"
			writeTree(t, dir, tc.files)
			report, err := lint.Run(t.Context(), dir, lint.Options{})
			if err != nil {
				t.Fatal(err)
			}
			for i := range report.Findings {
				if f := &report.Findings[i]; f.Design == "1.2-C" &&
					strings.Contains(f.Measured, tc.wantMsg) {
					return
				}
			}
			t.Fatalf("no 1.2-C finding saying %q in %+v", tc.wantMsg, report.Findings)
		})
	}
}

// writeTree writes files, by slash-separated path, under dir.
func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, src := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
