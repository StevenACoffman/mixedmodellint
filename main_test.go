package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runCLI drives the whole program, from argument parsing to the exit code,
// with injected I/O.
func runCLI(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = run(t.Context(), append([]string{"mixedmodellint"}, args...),
		strings.NewReader(""), &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestCheck(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		args     []string
		wantCode int
		wantOut  string
	}{
		"clean directory passes": {
			args: []string{
				"check",
				"--dir",
				filepath.Join("testdata", "e2e", "clean"),
			},
			wantCode: 0,
			wantOut:  "0 error(s)",
		},
		"MUST violation fails CI": {
			args: []string{
				"check",
				"--dir",
				filepath.Join("testdata", "e2e", "dirty"),
			},
			wantCode: 1,
			wantOut:  "[7.1-A]",
		},
		"sarif output": {
			args: []string{
				"check", "--format", "sarif", "--dir", filepath.Join("testdata", "e2e", "dirty"),
			},
			wantCode: 1, wantOut: `"version": "2.1.0"`,
		},
		"unknown format is a usage error": {
			args: []string{"check", "--format", "xml"}, wantCode: 2,
		},
		"positional argument is a usage error": {
			args: []string{"check", "somewhere"}, wantCode: 2,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			code, out, errOut := runCLI(t, tc.args...)
			if code != tc.wantCode {
				t.Fatalf(
					"exit %d, want %d\nstdout:\n%s\nstderr:\n%s",
					code,
					tc.wantCode,
					out,
					errOut,
				)
			}
			if !strings.Contains(out, tc.wantOut) {
				t.Fatalf("stdout does not contain %q:\n%s", tc.wantOut, out)
			}
		})
	}
}

func TestCheckBaseline(t *testing.T) {
	t.Parallel()
	dir := filepath.Join("testdata", "e2e", "dirty")
	baseline := filepath.Join(t.TempDir(), "mml-baseline.json")
	if code, out, errOut := runCLI(
		t,
		"check",
		"--dir",
		dir,
		"--write-baseline",
		baseline,
	); code != 0 {
		t.Fatalf("write baseline: exit %d\n%s%s", code, out, errOut)
	}
	code, out, errOut := runCLI(t, "check", "--dir", dir, "--baseline", baseline)
	if code != 0 {
		t.Fatalf("with baseline: exit %d, want 0\n%s%s", code, out, errOut)
	}
	if !strings.Contains(out, "in baseline") {
		t.Fatalf("summary does not report baselined findings:\n%s", out)
	}
}

func TestExplain(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		args     []string
		wantCode int
		wantOut  string
	}{
		"an implemented design": {
			args: []string{"explain", "7.1-B"}, wantOut: "7.1-B (implemented; reports at",
		},
		"a design not implemented": {
			args: []string{"explain", "2.1-A"}, wantOut: "2.1-A (not implemented in this build)",
		},
		"every design of a rule, with a section sign": {
			args: []string{"explain", "§7.1"}, wantOut: "7.1-C (",
		},
		"an unknown design is a usage error": {args: []string{"explain", "99.9-Z"}, wantCode: 2},
		"no argument is a usage error":       {args: []string{"explain"}, wantCode: 2},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			code, out, errOut := runCLI(t, tc.args...)
			if code != tc.wantCode {
				t.Fatalf("exit %d, want %d\nstdout:\n%s\nstderr:\n%s",
					code, tc.wantCode, out, errOut)
			}
			if !strings.Contains(out, tc.wantOut) {
				t.Fatalf("stdout does not contain %q:\n%s", tc.wantOut, out)
			}
		})
	}
}

func TestAnnotate(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		dir  string
		want []string
	}{
		"a table with only a key gets its grain filled in": {
			dir:  filepath.Join("testdata", "e2e", "dirty"),
			want: []string{"  order:\n", "grain: one row per order fp=", "definition: TODO"},
		},
		"a fully annotated table needs nothing": {
			dir: filepath.Join("testdata", "e2e", "clean"), want: []string{"entities: {}"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			code, out, errOut := runCLI(t, "annotate", "--dir", tc.dir)
			if code != 0 {
				t.Fatalf("exit %d\n%s%s", code, out, errOut)
			}
			for _, want := range tc.want {
				if !strings.Contains(out, want) {
					t.Errorf("stub does not contain %q:\n%s", want, out)
				}
			}
		})
	}
}

// TestLockThenCheck writes a lock for a copy of the clean fixture, checks
// that check accepts it, then changes the schema and checks that check
// reports the lock as stale.
func TestLockThenCheck(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(filepath.Join("testdata", "e2e", "clean"))); err != nil {
		t.Fatal(err)
	}
	if code, out, errOut := runCLI(t, "lock", "--dir", dir); code != 0 {
		t.Fatalf("lock: exit %d\n%s%s", code, out, errOut)
	}
	if code, out, errOut := runCLI(t, "check", "--dir", dir); code != 0 ||
		strings.Contains(out, "mml/lock-stale") {
		t.Fatalf("check with a fresh lock: exit %d\n%s%s", code, out, errOut)
	}
	schema := filepath.Join(dir, "schema.sql")
	src, err := os.ReadFile(schema)
	if err != nil {
		t.Fatal(err)
	}
	extra := "\nALTER TABLE orders ADD COLUMN note TEXT;\n"
	if err := os.WriteFile(schema, append(src, extra...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, out, _ := runCLI(t, "check", "--dir", dir); !strings.Contains(out, "mml/lock-stale") {
		t.Fatalf("check after a schema change does not report the stale lock:\n%s", out)
	}
}
