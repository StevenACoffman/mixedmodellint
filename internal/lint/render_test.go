package lint_test

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/StevenACoffman/mixedmodellint/internal/lint"
	mml "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

func equals(t *testing.T, exp, act any) {
	t.Helper()
	if !reflect.DeepEqual(exp, act) {
		t.Fatalf("expected %#v, got %#v", exp, act)
	}
}

func TestParseFormat(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		in      string
		want    lint.Format
		wantErr bool
	}{
		"text":    {"text", lint.FormatText, false},
		"json":    {"json", lint.FormatJSON, false},
		"upper":   {"SARIF", lint.FormatSARIF, false},
		"unknown": {"xml", 0, true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := lint.ParseFormat(tc.in)
			equals(t, tc.want, got)
			equals(t, tc.wantErr, err != nil)
			if err != nil {
				equals(t, mml.EINVALID, mml.ErrorCode(err))
			}
		})
	}
}

// TestRender checks that each format reaches its writer: the first bytes of
// the output identify the format.
func TestRender(t *testing.T) {
	t.Parallel()
	r := mml.Report{Findings: []mml.Finding{{
		Rule: "7.1", Design: "7.1-A", Severity: mml.SeverityError, Measured: "m", Suggestion: "s",
	}}}
	cases := map[string]struct {
		format   lint.Format
		prefix   string
		wantCode string
	}{
		"text":    {lint.FormatText, "<input>: error: [7.1-A]", ""},
		"json":    {lint.FormatJSON, "{\n  \"findings\"", ""},
		"sarif":   {lint.FormatSARIF, "{\n  \"$schema\"", ""},
		"unknown": {lint.Format(99), "", mml.EINVALID},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			err := lint.Render(&buf, r, tc.format, lint.Tool{Name: "mixedmodellint"})
			equals(t, tc.wantCode, mml.ErrorCode(err))
			if !strings.HasPrefix(buf.String(), tc.prefix) {
				t.Fatalf(
					"output starts %q, want %q",
					buf.String()[:min(len(buf.String()), 40)],
					tc.prefix,
				)
			}
		})
	}
}
