package lint_test

import (
	"fmt"
	"path/filepath"
	"slices"
	"testing"

	"github.com/StevenACoffman/mixedmodellint/internal/lint"
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
	var got []string
	for i := range report.Findings {
		f := &report.Findings[i]
		got = append(got, fmt.Sprintf("%s:%d %s", f.Pos.File, f.Pos.Line, f.Design))
	}
	want := []string{
		"app/app.go:15 7.2-A",
		"queries/fanout.sql:2 7.2-A",
		"queries/loads.sql:2 7.3-A",
		"queries/loads.sql:6 7.1-C",
		"queries/reads.sql:2 7.3-D",
		"queries/reads.sql:10 7.5-D",
	}
	if !slices.Equal(want, got) {
		t.Fatalf("findings:\n got  %q\n want %q", got, want)
	}
}
