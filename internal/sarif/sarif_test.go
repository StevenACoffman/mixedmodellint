package sarif_test

import (
	"bytes"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gosarif "github.com/owenrumney/go-sarif/v3/pkg/report/v210/sarif"

	mml "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
	"github.com/StevenACoffman/mixedmodellint/internal/sarif"
)

var update = flag.Bool("update", false, "update golden files")

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

// report has findings with every position shape (full, line only, file only,
// none) and two findings from one design, which must share a rule.
func report() mml.Report {
	return mml.Report{Findings: []mml.Finding{
		{
			Rule: "7.1", Design: "7.1-B", Title: "Grain and key consistency",
			Severity: mml.SeverityError, Pos: mml.Position{File: "db/schema.sql", Line: 10, Col: 3},
			Measured: "grain does not match the key", Suggestion: "Add UNIQUE (order_id).",
		},
		{
			Rule:       "5.1",
			Design:     "5.1-A",
			Title:      "Reserved-word and bare-type lexicon",
			Severity:   mml.SeverityWarning,
			Field:      "amount",
			Pos:        mml.Position{File: "db/schema.sql", Line: 17},
			Measured:   "amount is generic",
			Suggestion: "Rename amount.",
		},
		{
			Rule: "5.1", Design: "5.1-A", Title: "Reserved-word and bare-type lexicon",
			Severity: mml.SeverityNote, Field: "kind", Pos: mml.Position{File: "db/schema.sql"},
			Measured: "kind is weakly ambiguous", Suggestion: "Rename kind.",
		},
		{
			Rule: "mml", Design: "mml/parse-error", Title: "Source could not be read",
			Severity: mml.SeverityError, Measured: "no position at all", Suggestion: "Fix it.",
		},
	}}
}

func TestWrite(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	tool := sarif.Tool{
		Name:    "mixedmodellint",
		Version: "test",
		URI:     "https://github.com/StevenACoffman/mixedmodellint",
	}
	if err := sarif.Write(&buf, report(), tool); err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join("testdata", "report.sarif")
	if *update {
		if err := os.WriteFile(golden, buf.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Errorf("output mismatch\ngot:\n%s\nwant:\n%s", buf.Bytes(), want)
	}
}

func TestWriteFails(t *testing.T) {
	t.Parallel()
	err := sarif.Write(failingWriter{}, report(), sarif.Tool{Name: "x"})
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("error %v does not report the write failure", err)
	}
}

// TestWriteValidates checks the output against the SARIF 2.1.0 JSON schema,
// using go-sarif's own validator.
func TestWriteValidates(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := sarif.Write(&buf, report(), sarif.Tool{Name: "mixedmodellint"}); err != nil {
		t.Fatal(err)
	}
	parsed, err := gosarif.FromBytes(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if err := parsed.Validate(); err != nil {
		t.Fatalf("output is not valid SARIF 2.1.0: %v", err)
	}
}
