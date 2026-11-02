package mixedmodellint_test

import (
	"bytes"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

var update = flag.Bool("update", false, "update golden files")

// goldenReport is a fixed report covering each position shape: full, line
// only, file only, and none.
func goldenReport() m.Report {
	e := m.Entity{
		ID:     "sql:order_lines",
		Kind:   m.KindTable,
		Source: m.SourceSQL,
		Name:   "order_lines",
		Pos:    m.Position{File: "db/schema.sql", Line: 12, Col: 1},
		Annotations: m.Annotations{
			"grain": {Value: "one row per order", Pos: m.Position{File: "db/schema.sql", Line: 10}},
		},
		PrimaryKey: []string{"order_id", "line_no"},
		Refs:       []m.Ref{{Fields: []string{"order_id"}, Target: "orders"}},
		Fields: []m.Field{
			{Name: "order_id", Type: m.TypeInt},
			{Name: "line_no", Type: m.TypeInt},
			{
				Name: "date",
				Type: m.TypeDate,
				Pos:  m.Position{File: "db/schema.sql", Line: 15, Col: 3},
			},
			{
				Name: "kind",
				Type: m.TypeString,
				Pos:  m.Position{File: "db/schema.sql", Line: 16, Col: 3},
			},
			{
				Name: "amount",
				Type: m.TypeInt,
				Pos:  m.Position{File: "db/schema.sql", Line: 17, Col: 3},
			},
			{
				Name: "code", Type: m.TypeString,
				Annotations: m.Annotations{"waive": {Value: `5.1 reason="vendor code"`}},
			},
		},
	}
	parent := m.Entity{
		ID: "sql:orders", Kind: m.KindTable, Source: m.SourceSQL, Name: "orders",
		Annotations: m.Annotations{"grain": {Value: "one row per order"}},
		PrimaryKey:  []string{"order_id"},
		Fields:      []m.Field{{Name: "order_id", Type: m.TypeInt}},
	}
	r := m.Analyze(&m.Input{Sources: []m.Source{{
		Entities: []m.Entity{e, parent},
		Problems: []m.Problem{
			{
				Message: "syntax error at or near \"TABL\"",
				Pos:     m.Position{File: "db/bad.sql", Line: 3},
			},
			{Message: "file could not be read", Pos: m.Position{File: "db/empty.sql"}},
			{Message: "no position at all"},
		},
	}}})
	r.Findings = withoutContracts(r.Findings) // keep the golden files about rendering
	r.Baselined = 2                           // as if two more findings were in a baseline
	return r
}

func TestRender(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		write func(io.Writer, m.Report) error
		file  string
	}{
		"text": {m.WriteText, "report.txt"},
		"json": {m.WriteJSON, "report.json"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			ok(t, tc.write(&buf, goldenReport()))
			golden := filepath.Join("testdata", "render", tc.file)
			if *update {
				ok(t, os.MkdirAll(filepath.Dir(golden), 0o750))
				ok(t, os.WriteFile(golden, buf.Bytes(), 0o600))
			}
			want, err := os.ReadFile(golden)
			ok(t, err)
			if !bytes.Equal(buf.Bytes(), want) {
				t.Errorf("output mismatch for %s\ngot:\n%s\nwant:\n%s", golden, buf.Bytes(), want)
			}
		})
	}
}

// TestTextSummary pins the summary line for each mix of waived and
// baselined findings: the parenthesis appears when either is non-zero.
func TestTextSummary(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		r    m.Report
		want string
	}{
		"nothing hidden": {
			r:    m.Report{Entities: 2, Queries: 1},
			want: "0 error(s), 0 warning(s), 0 note(s) in 2 entities and 1 queries\n",
		},
		"waived only": {
			r:    m.Report{Waived: 1},
			want: "0 error(s), 0 warning(s), 0 note(s) in 0 entities and 0 queries (1 waived, 0 in baseline)\n",
		},
		"baselined only": {
			r:    m.Report{Baselined: 3},
			want: "0 error(s), 0 warning(s), 0 note(s) in 0 entities and 0 queries (0 waived, 3 in baseline)\n",
		},
		"both": {
			r:    m.Report{Waived: 1, Baselined: 1},
			want: "0 error(s), 0 warning(s), 0 note(s) in 0 entities and 0 queries (1 waived, 1 in baseline)\n",
		},
		"counts by severity": {r: m.Report{Findings: []m.Finding{
			{
				Severity: m.SeverityError,
			},
			{Severity: m.SeverityWarning},
			{Severity: m.SeverityWarning},
			{Severity: m.SeverityNote},
		}}, want: "1 error(s), 2 warning(s), 1 note(s) in 0 entities and 0 queries\n"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			ok(t, m.WriteText(&buf, tc.r))
			lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
			equals(t, tc.want, lines[len(lines)-1]+"\n")
		})
	}
}
