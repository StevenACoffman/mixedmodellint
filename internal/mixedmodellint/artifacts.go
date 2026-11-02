package mixedmodellint

import (
	"slices"
	"strings"
)

// Artifacts are the files other than model sources that designs read:
// decision records and logical-model records (rule_plan.md §1, "artifact
// corpus"). The zero value holds none, and the designs that need one report
// it missing.
type Artifacts struct {
	ADRs      []Document
	Logical   []LogicalRecord
	Ownership []OwnershipRecord
	// Codeowners are the principals CODEOWNERS names (@org/team, @user or an
	// email address), or nil when the repository has no CODEOWNERS.
	Codeowners []string
}

// Document is a text artifact, such as an architecture decision record.
type Document struct {
	Path string
	Text string
}

// LogicalRecord is a logical-model entry, model/logical/<entity>.yaml: the
// entity it describes, its attributes and its key, written before the
// physical model so the physical model can be checked against it (§1.1).
type LogicalRecord struct {
	Entity     string
	Attributes []string
	Keys       []string
	Pos        Position
}

// OwnershipRecord states what an owner is responsible for and may decide
// alone (rule_plan.md §1.13), from mml.yaml's owners section or
// model/ownership/<owner>.yaml. Model holds globs of the entities it covers.
// Liability is true for an explicit "ownership: liability" entry: an owner
// named without the authority to act.
type OwnershipRecord struct {
	Owner            string
	Model            []string
	Responsibilities []string
	Decisions        []Decision
	GrantedBy        string
	Liability        bool
	Pos              Position
}

// Decision is one decision an owner may make, and whether it needs anyone
// else's sign-off.
type Decision struct {
	Name           string
	WithoutSignoff bool
}

// ParseCodeowners returns the principals a CODEOWNERS file assigns: every
// owner after a path pattern, once each, in order. Comments and blank lines
// are skipped.
func ParseCodeowners(text string) []string {
	out := []string{}
	for line := range strings.Lines(text) {
		line, _, _ = strings.Cut(line, "#")
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		for _, owner := range fields[1:] {
			if !slices.Contains(out, owner) {
				out = append(out, owner)
			}
		}
	}
	return out
}
