package mixedmodellint

// Artifacts are the files other than model sources that designs read:
// decision records and logical-model records (rule_plan.md §1, "artifact
// corpus"). The zero value holds none, and the designs that need one report
// it missing.
type Artifacts struct {
	ADRs      []Document
	Logical   []LogicalRecord
	Ownership []OwnershipRecord
	// Codeowners is the repository's CODEOWNERS file, or nil when it has
	// none.
	Codeowners *Codeowners
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
