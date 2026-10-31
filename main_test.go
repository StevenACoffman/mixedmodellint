package main

import (
	"bytes"
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
