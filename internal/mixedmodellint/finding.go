package mixedmodellint

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"
)

// Strengths.
const (
	Must Strength = iota + 1
	Should
	Consider
)

// Severities, ordered from least to most severe.
const (
	SeverityNote Severity = iota + 1
	SeverityWarning
	SeverityError
)

// Strength is a rule's modal strength in data_model_rules.md.
type Strength int

// Severity is how a finding is reported. It follows the rule's strength
// (MUST is an error, SHOULD a warning, CONSIDER a note) and is one level
// lower for a heuristic design (rule_plan.md §3).
type Severity int

// Finding is one measured violation of one rule by one entity or field.
//
// Measured states the observed value ("key (order_id, line_no) has no column
// for 'customer'"), never only "violates rule". Suggestion says what to change,
// with the names filled in.
type Finding struct {
	Rule       string // "7.1", or "mml" for the linter's own findings
	Design     string // "7.1-B", or "mml/unknown-key"
	Title      string // the design's name, for SARIF rule metadata
	Severity   Severity
	EntityID   string
	Entity     string
	Field      string
	Pos        Position
	Measured   string
	Suggestion string
}

// String returns the SARIF level name: "note", "warning" or "error".
func (s Severity) String() string {
	switch s {
	case SeverityNote:
		return "note"
	case SeverityWarning:
		return "warning"
	case SeverityError:
		return "error"
	default:
		return "none"
	}
}

// Fingerprint identifies a finding across runs for the baseline. It hashes
// the design, entity, field and measured text, and deliberately not the
// position, so a finding keeps its fingerprint when lines above it move.
func (f *Finding) Fingerprint() string {
	sum := sha256.Sum256([]byte(strings.Join(
		[]string{f.Design, f.EntityID, f.Field, f.Measured}, "\x00")))
	return hex.EncodeToString(sum[:8])
}

// sortFindings orders findings by file, line, column, design and entity, so
// output is identical from run to run.
func sortFindings(fs []Finding) {
	slices.SortStableFunc(fs, func(a, b Finding) int {
		return cmp.Or(
			cmp.Compare(a.Pos.File, b.Pos.File),
			cmp.Compare(a.Pos.Line, b.Pos.Line),
			cmp.Compare(a.Pos.Col, b.Pos.Col),
			cmp.Compare(a.Design, b.Design),
			cmp.Compare(a.EntityID, b.EntityID),
			cmp.Compare(a.Field, b.Field),
			cmp.Compare(a.Measured, b.Measured),
		)
	})
}
