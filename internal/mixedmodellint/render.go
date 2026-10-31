package mixedmodellint

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type jsonReport struct {
	Findings []jsonFinding `json:"findings"`
	Summary  jsonSummary   `json:"summary"`
}

type jsonFinding struct {
	Rule        string `json:"rule"`
	Design      string `json:"design"`
	Severity    string `json:"severity"`
	Entity      string `json:"entity,omitempty"`
	EntityID    string `json:"entity_id,omitempty"`
	Field       string `json:"field,omitempty"`
	File        string `json:"file,omitempty"`
	Line        int    `json:"line,omitempty"`
	Column      int    `json:"column,omitempty"`
	Measured    string `json:"measured"`
	Suggestion  string `json:"suggestion"`
	Fingerprint string `json:"fingerprint"`
}

type jsonSummary struct {
	Errors    int `json:"errors"`
	Warnings  int `json:"warnings"`
	Notes     int `json:"notes"`
	Entities  int `json:"entities"`
	Queries   int `json:"queries"`
	Waived    int `json:"waived"`
	Baselined int `json:"baselined"`
}

// WriteText writes r to w as text: one line per finding with its position,
// severity, design and measured value, the suggestion indented beneath it, and
// a summary line.
func WriteText(w io.Writer, r Report) error {
	var b strings.Builder
	for i := range r.Findings {
		f := &r.Findings[i]
		fmt.Fprintf(&b, "%s: %s: [%s] %s\n", f.Pos, f.Severity, f.Design, f.Measured)
		fmt.Fprintf(&b, "    suggestion: %s\n", f.Suggestion)
	}
	s := summarize(r)
	fmt.Fprintf(&b, "%d error(s), %d warning(s), %d note(s) in %d entities and %d queries",
		s.Errors, s.Warnings, s.Notes, s.Entities, s.Queries)
	if s.Waived > 0 || s.Baselined > 0 {
		fmt.Fprintf(&b, " (%d waived, %d in baseline)", s.Waived, s.Baselined)
	}
	b.WriteString("\n")
	_, err := io.WriteString(w, b.String())
	if err != nil {
		return fmt.Errorf("write text report: %w", err)
	}
	return nil
}

func summarize(r Report) jsonSummary {
	s := jsonSummary{
		Entities: r.Entities, Queries: r.Queries, Waived: r.Waived, Baselined: r.Baselined,
	}
	for i := range r.Findings {
		switch r.Findings[i].Severity {
		case SeverityError:
			s.Errors++
		case SeverityWarning:
			s.Warnings++
		case SeverityNote:
			s.Notes++
		default:
		}
	}
	return s
}

func toJSON(r Report) jsonReport {
	out := jsonReport{Findings: make([]jsonFinding, 0, len(r.Findings)), Summary: summarize(r)}
	for i := range r.Findings {
		f := &r.Findings[i]
		out.Findings = append(out.Findings, jsonFinding{
			Rule: f.Rule, Design: f.Design, Severity: f.Severity.String(),
			Entity: f.Entity, EntityID: f.EntityID, Field: f.Field,
			File: f.Pos.File, Line: f.Pos.Line, Column: f.Pos.Col,
			Measured: f.Measured, Suggestion: f.Suggestion, Fingerprint: f.Fingerprint(),
		})
	}
	return out
}

// WriteJSON writes r to w as an indented JSON document with a "findings"
// array and a "summary" object.
func WriteJSON(w io.Writer, r Report) error {
	return writeJSON(w, toJSON(r))
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("encode json: %w", err)
	}
	return nil
}
