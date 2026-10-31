package mixedmodellint

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Output formats.
const (
	FormatText Format = iota + 1
	FormatJSON
	FormatSARIF
)

// sarifSchema is the SARIF 2.1.0 JSON schema URI GitHub code scanning expects.
const sarifSchema = "https://json.schemastore.org/sarif-2.1.0.json"

// Format is an output format for Render.
type Format int

// Tool identifies the program in SARIF output.
type Tool struct {
	Name    string
	Version string
	URI     string
}

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

type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	Version        string      `json:"version,omitempty"`
	InformationURI string      `json:"informationUri,omitempty"`
	Rules          []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID                   string      `json:"id"`
	Name                 string      `json:"name"`
	ShortDescription     sarifText   `json:"shortDescription"`
	DefaultConfiguration sarifConfig `json:"defaultConfiguration"`
}

type sarifConfig struct {
	Level string `json:"level"`
}

type sarifText struct {
	Text string `json:"text"`
}

type sarifResult struct {
	RuleID              string            `json:"ruleId"`
	RuleIndex           int               `json:"ruleIndex"`
	Level               string            `json:"level"`
	Message             sarifText         `json:"message"`
	Locations           []sarifLocation   `json:"locations,omitempty"`
	PartialFingerprints map[string]string `json:"partialFingerprints"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysical `json:"physicalLocation"`
}

type sarifPhysical struct {
	ArtifactLocation sarifArtifact `json:"artifactLocation"`
	Region           *sarifRegion  `json:"region,omitempty"`
}

type sarifArtifact struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine   int `json:"startLine"`
	StartColumn int `json:"startColumn,omitempty"`
}

// ParseFormat returns the Format named by s: "text", "json" or "sarif".
func ParseFormat(s string) (Format, error) {
	switch strings.ToLower(s) {
	case "text":
		return FormatText, nil
	case "json":
		return FormatJSON, nil
	case "sarif":
		return FormatSARIF, nil
	default:
		return 0, &Error{
			Code:    EINVALID,
			Message: fmt.Sprintf("unknown format %q (want text, json or sarif)", s),
		}
	}
}

// Render writes r to w in format f. tool names the program in SARIF output and
// is ignored by the other formats.
func Render(w io.Writer, r Report, f Format, tool Tool) error {
	const op = "mixedmodellint.Render"
	var err error
	switch f {
	case FormatText:
		err = renderText(w, r)
	case FormatJSON:
		err = writeJSON(w, toJSON(r))
	case FormatSARIF:
		err = writeJSON(w, toSARIF(r, tool))
	default:
		return &Error{Op: op, Err: &Error{Code: EINVALID, Message: "unknown format"}}
	}
	if err != nil {
		return &Error{Op: op, Err: err}
	}
	return nil
}

func renderText(w io.Writer, r Report) error {
	var b strings.Builder
	for i := range r.Findings {
		f := &r.Findings[i]
		fmt.Fprintf(&b, "%s: %s: [%s] %s\n", location(f.Pos), f.Severity, f.Design, f.Measured)
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

func location(p Position) string {
	switch {
	case p.File == "":
		return "<input>"
	case p.Line == 0:
		return p.File
	case p.Col == 0:
		return fmt.Sprintf("%s:%d", p.File, p.Line)
	default:
		return fmt.Sprintf("%s:%d:%d", p.File, p.Line, p.Col)
	}
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

// toSARIF builds a SARIF 2.1.0 log with one rule per design that produced a
// finding. partialFingerprints carries the baseline fingerprint, so GitHub
// code scanning tracks a finding across commits the same way the baseline does.
func toSARIF(r Report, tool Tool) sarifLog {
	driver := sarifDriver{
		Name: tool.Name, Version: tool.Version, InformationURI: tool.URI,
		Rules: []sarifRule{},
	}
	index := map[string]int{}
	results := make([]sarifResult, 0, len(r.Findings))
	for i := range r.Findings {
		f := &r.Findings[i]
		ri, ok := index[f.Design]
		if !ok {
			ri = len(driver.Rules)
			index[f.Design] = ri
			driver.Rules = append(driver.Rules, sarifRule{
				ID: f.Design, Name: f.Title,
				ShortDescription:     sarifText{Text: ruleLabel(f) + " " + f.Title},
				DefaultConfiguration: sarifConfig{Level: f.Severity.String()},
			})
		}
		results = append(results, sarifResult{
			RuleID: f.Design, RuleIndex: ri, Level: f.Severity.String(),
			Message:             sarifText{Text: f.Measured + ". " + f.Suggestion},
			Locations:           sarifLocations(f.Pos),
			PartialFingerprints: map[string]string{"mixedmodellint/v1": f.Fingerprint()},
		})
	}
	return sarifLog{
		Schema: sarifSchema, Version: "2.1.0",
		Runs: []sarifRun{{Tool: sarifTool{Driver: driver}, Results: results}},
	}
}

func ruleLabel(f *Finding) string {
	if f.Rule == "mml" {
		return "mixedmodellint:"
	}
	return "§" + f.Rule + ":"
}

func sarifLocations(p Position) []sarifLocation {
	if p.File == "" {
		return nil
	}
	loc := sarifLocation{
		PhysicalLocation: sarifPhysical{ArtifactLocation: sarifArtifact{URI: p.File}},
	}
	if p.Line > 0 {
		loc.PhysicalLocation.Region = &sarifRegion{StartLine: p.Line, StartColumn: p.Col}
	}
	return []sarifLocation{loc}
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
