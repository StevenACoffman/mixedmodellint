package mixedmodellint

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
)

// baselineVersion is the format version written to and required of a
// baseline file.
const baselineVersion = 1

// Baseline is the set of finding fingerprints an existing codebase accepted
// when it adopted the linter. Findings in it are not reported, so CI fails
// only on new violations (rule_plan.md §3). The zero Baseline is empty.
type Baseline struct {
	fingerprints map[string]bool
}

type baselineFile struct {
	Version      int      `json:"version"`
	Fingerprints []string `json:"fingerprints"`
}

// ReadBaseline decodes a baseline file written by WriteBaseline.
func ReadBaseline(r io.Reader) (Baseline, error) {
	const op = "mixedmodellint.ReadBaseline"
	var bf baselineFile
	if err := json.NewDecoder(r).Decode(&bf); err != nil {
		return Baseline{}, &Error{Op: op, Err: &Error{
			Code: EINVALID, Message: fmt.Sprintf("baseline is not valid JSON: %v", err),
		}}
	}
	if bf.Version != baselineVersion {
		return Baseline{}, &Error{Op: op, Err: &Error{
			Code: EINVALID,
			Message: fmt.Sprintf(
				"baseline version %d is not supported (want %d)",
				bf.Version,
				baselineVersion,
			),
		}}
	}
	b := Baseline{fingerprints: make(map[string]bool, len(bf.Fingerprints))}
	for _, fp := range bf.Fingerprints {
		b.fingerprints[fp] = true
	}
	return b, nil
}

// WriteBaseline writes the fingerprints of r's findings, sorted, so that
// regenerating an unchanged baseline produces an identical file.
func WriteBaseline(w io.Writer, r Report) error {
	fps := make([]string, 0, len(r.Findings))
	for i := range r.Findings {
		fps = append(fps, r.Findings[i].Fingerprint())
	}
	slices.Sort(fps)
	fps = slices.Compact(fps)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(baselineFile{Version: baselineVersion, Fingerprints: fps}); err != nil {
		return &Error{Op: "mixedmodellint.WriteBaseline", Err: err}
	}
	return nil
}

// filter returns the findings whose fingerprints are not in b, and how many
// were removed.
func (b Baseline) filter(findings []Finding) (kept []Finding, removed int) {
	for i := range findings {
		if b.fingerprints[findings[i].Fingerprint()] {
			removed++
			continue
		}
		kept = append(kept, findings[i])
	}
	return kept, removed
}
