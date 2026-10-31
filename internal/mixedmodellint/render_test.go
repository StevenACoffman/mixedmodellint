package mixedmodellint_test

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
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
	r := m.Analyze(m.Input{Sources: []m.Source{{
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
	r.Baselined = 2 // as if two more findings were in a baseline
	return r
}

func TestRender(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		format m.Format
		file   string
	}{
		"text":  {m.FormatText, "report.txt"},
		"json":  {m.FormatJSON, "report.json"},
		"sarif": {m.FormatSARIF, "report.sarif"},
	}
	tool := m.Tool{
		Name:    "mixedmodellint",
		Version: "test",
		URI:     "https://github.com/StevenACoffman/mixedmodellint",
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			ok(t, m.Render(&buf, goldenReport(), tc.format, tool))
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

func TestParseFormat(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		in      string
		want    m.Format
		wantErr bool
	}{
		"text":    {"text", m.FormatText, false},
		"upper":   {"SARIF", m.FormatSARIF, false},
		"unknown": {"xml", 0, true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := m.ParseFormat(tc.in)
			equals(t, tc.want, got)
			equals(t, tc.wantErr, err != nil)
			if err != nil {
				equals(t, m.EINVALID, m.ErrorCode(err))
			}
		})
	}
}
