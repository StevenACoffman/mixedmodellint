package git_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/StevenACoffman/mixedmodellint/internal/git"
	mml "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// TestCheckRef pins CheckRef to git check-ref-format --allow-onelevel: every
// case but the leading dash, which CheckRef rejects so a ref is never read as
// an option, was checked against git 2.49.
func TestCheckRef(t *testing.T) {
	t.Parallel()
	valid := []string{
		"HEAD", "main", "origin/main", "refs/heads/-foo", "v1.2", "a@b", "é",
		"0123456789abcdef0123456789abcdef01234567", "feature/a.b",
	}
	invalid := []string{
		"", "@", "-foo", "--upload-pack=x", "/a", "a/", "a//b", "a.", "a..b", "a@{b",
		"@{", ".a", "a/.b", "a.lock", "a.lock/b", "a/b.lock/c", "a b", "a\nb",
		"a\tb", "a\x7fb", "a~1", "HEAD^", "a:b", "a?", "a*", "a[", `a\b`, "\xff",
	}
	for _, ref := range valid {
		if err := git.CheckRef(ref); err != nil {
			t.Errorf("CheckRef(%q) = %v, want nil", ref, err)
		}
	}
	for _, ref := range invalid {
		if err := git.CheckRef(ref); mml.ErrorCode(err) != mml.EINVALID {
			t.Errorf("CheckRef(%q) = %v, want an EINVALID error", ref, err)
		}
	}
}

// TestExport exports a subdirectory at an older commit and checks that only
// that subtree, as committed then, is written.
func TestExport(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	repo := initRepo(t)
	sub := filepath.Join(repo, "db")
	write(t, filepath.Join(sub, "nested", "a.sql"), "old")
	write(t, filepath.Join(repo, "outside.sql"), "outside")
	run(t, repo, exec.CommandContext(ctx, "git", "add", "."))
	run(t, repo, exec.CommandContext(ctx, "git", "commit", "-qm", "base"))
	run(t, repo, exec.CommandContext(ctx, "git", "tag", "base"))
	write(t, filepath.Join(sub, "nested", "a.sql"), "new")

	dst := t.TempDir()
	if err := git.Export(ctx, sub, "base", dst); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dst, "nested", "a.sql"))
	if err != nil || string(got) != "old" {
		t.Fatalf("nested/a.sql = %q, %v; want the committed %q", got, err, "old")
	}
	if _, err := os.Stat(filepath.Join(dst, "outside.sql")); !os.IsNotExist(err) {
		t.Fatalf("a file outside the directory was exported: %v", err)
	}
	for _, ref := range []string{"missing", "HEAD~1"} {
		err := git.Export(ctx, sub, ref, t.TempDir())
		if mml.ErrorCode(err) != mml.EINVALID {
			t.Errorf("Export(%q) = %v, want an EINVALID error", ref, err)
		}
	}
}

// initRepo returns a new git repository, skipping the test without git.
func initRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	repo := t.TempDir()
	run(t, repo, exec.CommandContext(t.Context(), "git", "init", "-q"))
	return repo
}

// run runs a git command in dir, isolated from the user's git config.
func run(t *testing.T, dir string, cmd *exec.Cmd) {
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

func write(t *testing.T, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
