package mixedmodellint_test

import (
	"errors"
	"io"
	"strings"
	"testing"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// failingWriter fails every write, as a full disk or closed pipe does.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestWriteErrors(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		write func(io.Writer, m.Report) error
	}{
		"text write fails": {m.WriteText},
		"json write fails": {m.WriteJSON},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := tc.write(failingWriter{}, goldenReport())
			if err == nil || !strings.Contains(err.Error(), "disk full") {
				t.Fatalf("error %v does not report the write failure", err)
			}
		})
	}
}

func TestErrorHelpers(t *testing.T) {
	t.Parallel()
	leaf := &m.Error{Code: m.ENOTFOUND, Message: "no such entity"}
	cases := map[string]struct {
		err         error
		wantCode    string
		wantMessage string
		wantText    string
	}{
		"nil":  {nil, "", "", ""},
		"leaf": {leaf, m.ENOTFOUND, "no such entity", "no such entity"},
		"wrapped": {
			&m.Error{Op: "a.B", Err: leaf},
			m.ENOTFOUND,
			"no such entity",
			"a.B: no such entity",
		},
		"foreign":   {errors.New("boom"), m.EINTERNAL, "internal error", "boom"},
		"code only": {&m.Error{Code: m.EINVALID}, m.EINVALID, "internal error", "invalid"},
		"wraps plain": {
			&m.Error{Op: "x.Y", Err: errors.New("io")},
			m.EINTERNAL,
			"internal error",
			"x.Y: io",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.wantCode, m.ErrorCode(tc.err))
			equals(t, tc.wantMessage, m.ErrorMessage(tc.err))
			if tc.err != nil {
				equals(t, tc.wantText, tc.err.Error())
			}
		})
	}
}

func TestLinterFindingFields(t *testing.T) {
	t.Parallel()
	pos := m.Position{File: "db/a.sql", Line: 7, Col: 3}
	e := m.Entity{
		ID: "sql:carts", Kind: m.KindTable, Source: m.SourceSQL, Name: "carts",
		Annotations: m.Annotations{"grain": {Value: "one row per cart", Pos: pos}},
		PrimaryKey:  []string{"cart_id"},
		Fields: []m.Field{
			{Name: "cart_id", Type: m.TypeInt},
			{
				Name:        "note",
				Type:        m.TypeString,
				Annotations: m.Annotations{"colour": {Value: "x", Pos: pos}},
			},
			{
				Name:        "amount",
				Type:        m.TypeInt,
				Annotations: m.Annotations{"waive": {Value: `5.1 reason="legacy"`}},
			},
		},
	}
	r := m.Analyze(m.Input{
		Sources: []m.Source{
			{Entities: []m.Entity{e}},
			{
				Problems: []m.Problem{{Message: "bad", Pos: pos}},
				Queries:  []m.Query{{Kind: m.QuerySelect}},
			},
		},
		Sidecar: m.Sidecar{File: "mml.yaml", Entities: map[string]m.SidecarEntity{
			"sql:carts": {Annotations: m.Annotations{"grain": {
				Value: "one row per basket",
				Pos:   m.Position{File: "mml.yaml", Line: 3},
			}}},
		}},
	})
	byDesign := map[string]m.Finding{}
	for i := range r.Findings {
		byDesign[r.Findings[i].Design] = r.Findings[i]
	}
	conflict, unknown, parse := byDesign["mml/annotation-conflict"], byDesign["mml/unknown-key"],
		byDesign["mml/parse-error"]
	cases := map[string]struct{ exp, act any }{
		"counts entities":       {1, r.Entities},
		"counts queries":        {1, r.Queries},
		"field waiver applied":  {1, r.Waived},
		"three linter findings": {3, len(r.Findings)},
		"conflict names both": {
			`grain is "one row per cart" inline but "one row per basket" in mml.yaml`,
			conflict.Measured,
		},
		"conflict at inline":    {pos, conflict.Pos},
		"conflict is a warning": {m.SeverityWarning, conflict.Severity},
		"conflict entity":       {"carts", conflict.Entity},
		"unknown key on field":  {"note", unknown.Field},
		"unknown key text": {
			`annotation key "colour" is not a known mml key`,
			unknown.Measured,
		},
		"unknown key is a note":   {m.SeverityNote, unknown.Severity},
		"parse error position":    {pos, parse.Pos},
		"parse error is an error": {m.SeverityError, parse.Severity},
		"parse error rule":        {"mml", parse.Rule},
		"parse error message":     {"bad", parse.Measured},
		"conflict rule":           {"mml", conflict.Rule},
		"conflict entity id":      {"sql:carts", conflict.EntityID},
		"conflict entity field":   {"", conflict.Field},
		"conflict suggestion": {
			"Keep one of the two declarations of grain; the inline value is the one in effect.",
			conflict.Suggestion,
		},
		"conflict title":        {"Inline annotation disagrees with mml.yaml", conflict.Title},
		"unknown key rule":      {"mml", unknown.Rule},
		"unknown key entity id": {"sql:carts", unknown.EntityID},
		"unknown key entity":    {"carts", unknown.Entity},
		"unknown key position":  {pos, unknown.Pos},
		"unknown key suggestion": {
			"Check the spelling against rule_plan.md §2; no rule reads this key.",
			unknown.Suggestion,
		},
		"unknown key title": {"Annotation key is not in the vocabulary", unknown.Title},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.exp, tc.act)
		})
	}
}

func TestSidecarAndLinking(t *testing.T) {
	t.Parallel()
	vendor := m.Entity{
		ID: "sql:vendor_orders", Kind: m.KindTable, Source: m.SourceSQL, Name: "vendor_orders",
		PrimaryKey: []string{"vendor_order_id"},
		Fields: []m.Field{
			{Name: "vendor_order_id", Type: m.TypeInt},
			{Name: "amount", Type: m.TypeInt},
		},
	}
	linked := m.Entity{
		ID: "go:model.OrderRow", Kind: m.KindStruct, Source: m.SourceGo, Name: "OrderRow",
		Annotations: m.Annotations{"entity": {Value: "vendor_orders"}},
		Fields:      []m.Field{{Name: "vendor_order_id", Type: m.TypeInt}},
	}
	sidecar := m.Sidecar{File: "mml.yaml", Entities: map[string]m.SidecarEntity{
		"vendor_orders": {
			Annotations: m.Annotations{"grain": {Value: "one row per vendor order"}},
			Fields: map[string]m.Annotations{
				"AMOUNT": {"waive": {Value: `5.1 reason="vendor column name"`}},
			},
		},
	}}
	r := m.Analyze(m.Input{
		Sources: []m.Source{{Entities: []m.Entity{vendor, linked}}}, Sidecar: sidecar,
	})
	equals(t, 0, len(r.Findings))
	equals(t, 1, r.Waived)
	equals(t, false, r.HasErrors())

	// Without the sidecar, the grain is missing on both entities, since the
	// Go struct is linked to the table through its entity annotation.
	bare := m.Analyze(m.Input{Sources: []m.Source{{Entities: []m.Entity{vendor, linked}}}})
	var missing []string
	for i := range bare.Findings {
		if bare.Findings[i].Design == "7.1-A" {
			missing = append(missing, bare.Findings[i].Entity)
		}
	}
	equals(t, []string{"OrderRow", "vendor_orders"}, missing)
}

func TestHasErrors(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		severities []m.Severity
		want       bool
	}{
		"empty":         {nil, false},
		"warnings only": {[]m.Severity{m.SeverityWarning, m.SeverityNote}, false},
		"one error":     {[]m.Severity{m.SeverityNote, m.SeverityError}, true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var r m.Report
			for _, s := range tc.severities {
				r.Findings = append(r.Findings, m.Finding{Severity: s})
			}
			equals(t, tc.want, r.HasErrors())
		})
	}
}
