package mixedmodellint

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
)

func section1Designs() []design {
	return []design{
		{id: "1.1-B", title: "New-table gate", strength: Must, check: check11B},
		{
			id: "1.2-A", title: "Definition, owner and escalation contract", strength: Must,
			check: check12A,
		},
		{id: "1.13-A", title: "Ownership-record contract", strength: Must, check: check113A},
	}
}

// check11B reports entities that are new since the previous model and have
// no logical-model record or ADR (rule_plan.md §1.1-B). Without a previous
// model nothing is new, so nothing is reported.
func check11B(c *catalog) []issue {
	if c.previous == nil {
		return nil
	}
	before := map[string]bool{}
	for _, e := range c.previous {
		before[e.ID] = true
	}
	var out []issue
	for _, e := range c.entities {
		if before[e.ID] || modelGateExempt(e) {
			continue
		}
		if is, ok := c.newEntityIssue(e); ok {
			out = append(out, is)
		}
	}
	return out
}

// modelGateExempt reports entities the gate does not apply to: views, whose
// shape is their query's; data the team does not own; and scratch work.
func modelGateExempt(e *Entity) bool {
	life, _ := e.Annotations.Get("lifecycle")
	return e.Kind == KindView || foreignSchema(e) || life == "scratch"
}

func (c *catalog) newEntityIssue(e *Entity) (issue, bool) {
	if rec, ok := c.logicalRecord(e); ok {
		return logicalDisagreement(e, rec)
	}
	if c.adrNames(e) {
		return issue{}, false
	}
	return issue{
		entity: e,
		measured: fmt.Sprintf("%s is new since mml.lock.json and has no logical-model record "+
			"or ADR naming it", e.Name),
		suggestion: fmt.Sprintf("Write model/logical/%s.yaml with its attributes, keys and "+
			"cardinality (entity: %s, attributes: [...], keys: [...]), or reference %s in an "+
			"ADR under docs/adr/ in this change (§1.1).", e.Canonical, e.Canonical, e.Canonical),
	}, true
}

func (c *catalog) logicalRecord(e *Entity) (*LogicalRecord, bool) {
	for i := range c.artifacts.Logical {
		if Canonical(c.artifacts.Logical[i].Entity) == e.Canonical {
			return &c.artifacts.Logical[i], true
		}
	}
	return nil, false
}

// adrNames reports whether an ADR mentions e by its name or canonical name
// as a whole word.
func (c *catalog) adrNames(e *Entity) bool {
	rx := regexp.MustCompile(`(?i)\b(` + regexp.QuoteMeta(e.Name) + `|` +
		regexp.QuoteMeta(e.Canonical) + `)\b`)
	for _, d := range c.artifacts.ADRs {
		if rx.MatchString(d.Text) {
			return true
		}
	}
	return false
}

// logicalDisagreement reports a logical record that does not describe e:
// an attribute with no column, or keys other than e's primary key. A record
// copied from another entity fails here.
func logicalDisagreement(e *Entity, rec *LogicalRecord) (issue, bool) {
	var missing []string
	for _, a := range rec.Attributes {
		if _, ok := e.Field(a); !ok {
			missing = append(missing, a)
		}
	}
	keysDiffer := len(rec.Keys) > 0 && !sameColumns(rec.Keys, e.PrimaryKey)
	if len(missing) == 0 && !keysDiffer {
		return issue{}, false
	}
	var parts []string
	if len(missing) > 0 {
		parts = append(parts, "attributes with no column: "+strings.Join(missing, ", "))
	}
	if keysDiffer {
		parts = append(parts, fmt.Sprintf("logical keys (%s) differ from the primary key (%s)",
			strings.Join(rec.Keys, ", "), strings.Join(e.PrimaryKey, ", ")))
	}
	return issue{
		entity: e, pos: rec.Pos,
		measured: fmt.Sprintf("the logical-model record for %s does not match it: %s", e.Name,
			strings.Join(parts, "; ")),
		suggestion: fmt.Sprintf("Update the record or the physical model of %s so they agree, "+
			"or record the deviation (§1.9).", e.Name),
	}, true
}

// governed reports whether e is core model the team owns, which needs a
// definition and an owner: not scratch work and not someone else's data.
func governed(e *Entity) bool {
	src, _ := e.Annotations.Get("source")
	life, _ := e.Annotations.Get("lifecycle")
	return life != "scratch" && src != "third_party"
}

func check12A(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		if !governed(e) {
			continue
		}
		if problems := c.definitionProblems(e); len(problems) > 0 {
			out = append(out, issue{
				entity: e,
				measured: fmt.Sprintf("the definition contract of %s is incomplete: %s", e.Name,
					strings.Join(problems, "; ")),
				suggestion: fmt.Sprintf("Declare definition (a sentence of at least eight words "+
					"saying what one %s is), owner (team:<x>, role:<x> or a CODEOWNERS "+
					"principal) and escalation (someone other than the owner who settles "+
					"disputes) (§1.2).", e.Canonical),
			})
		}
		if is, ok := staleDefinition(e); ok {
			out = append(out, is)
		}
	}
	return out
}

// definitionProblems lists what e's definition, owner and escalation lack.
func (c *catalog) definitionProblems(e *Entity) []string {
	var problems []string
	def, hasDef := e.Annotations.Get("definition")
	owner, hasOwner := e.Annotations.Get("owner")
	esc, hasEsc := e.Annotations.Get("escalation")
	switch text, _ := splitParams(def); {
	case !hasDef:
		problems = append(problems, "no definition")
	case !substantialDefinition(c, e, text):
		problems = append(problems, "the definition is too thin to settle a dispute")
	}
	if p := c.principalProblem("owner", owner, hasOwner); p != "" {
		problems = append(problems, p)
	}
	if p := c.principalProblem("escalation", esc, hasEsc); p != "" {
		problems = append(problems, p)
	}
	if hasOwner && hasEsc && strings.EqualFold(owner, esc) {
		problems = append(problems, "the escalation is the owner, so no one else can settle "+
			"a dispute")
	}
	return problems
}

// principalProblem says what is wrong with an owner or escalation, or "":
// it must be present, be a team, group, role or CODEOWNERS principal, and,
// when it is a CODEOWNERS principal and the repository has CODEOWNERS, be
// listed there.
func (c *catalog) principalProblem(key, value string, has bool) string {
	switch {
	case !has:
		return "no " + key
	case !c.rx.principal.MatchString(value):
		return fmt.Sprintf("%s %q is not a team, group, role or CODEOWNERS principal", key, value)
	case strings.HasPrefix(value, "@") && c.artifacts.Codeowners != nil &&
		!slices.Contains(c.artifacts.Codeowners, value):
		return fmt.Sprintf("%s %s is not in CODEOWNERS", key, value)
	default:
		return ""
	}
}

// substantialDefinition applies §1.2-A's quality floor: at least eight words,
// four of them content words that are not the entity's own name, and not a
// placeholder.
func substantialDefinition(c *catalog, e *Entity, text string) bool {
	if c.rx.placeholderDefinition.MatchString(text) {
		return false
	}
	words := strings.Fields(text)
	own := splitWords(e.Name)
	content := 0
	for _, w := range words {
		w = strings.ToLower(strings.Trim(w, ".,;:()\"'"))
		if w != "" && !stopword(w) && !slices.Contains(own, w) &&
			!slices.Contains(own, singular(w)) {
			content++
		}
	}
	return len(words) >= 8 && content >= 4
}

func stopword(w string) bool {
	switch w {
	case "a", "an", "the", "of", "to", "in", "on", "for", "by", "with", "and", "or", "is", "are",
		"be", "it", "its", "that", "this", "as", "at", "from", "one", "each", "per", "which", "who":
		return true
	default:
		return false
	}
}

// staleDefinition reports a definition whose fp= no longer matches e's shape
// (M1): the entity changed since the definition was written.
func staleDefinition(e *Entity) (issue, bool) {
	def, ok := e.Annotations.Get("definition")
	if !ok {
		return issue{}, false
	}
	_, params := splitParams(def)
	fp, has := params["fp"]
	now := shapeFingerprint(e)
	if !has || fp == now {
		return issue{}, false
	}
	return issue{
		entity: e, pos: e.Annotations["definition"].Pos,
		measured: fmt.Sprintf("%s changed shape since its definition was written (fp=%s, now "+
			"%s) (mml/stale-declaration)", e.Name, fp, now),
		suggestion: fmt.Sprintf("Re-read the definition of %s against its fields, then update "+
			"it to fp=%s.", e.Name, now),
	}, true
}

func check113A(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		owner, ok := e.Annotations.Get("owner")
		if !ok || !governed(e) {
			continue
		}
		rec := c.ownershipRecord(owner)
		if is, ok := ownershipIssue(e, owner, rec); ok {
			out = append(out, is)
		}
	}
	return out
}

func (c *catalog) ownershipRecord(owner string) *OwnershipRecord {
	for i := range c.artifacts.Ownership {
		if strings.EqualFold(c.artifacts.Ownership[i].Owner, owner) {
			return &c.artifacts.Ownership[i]
		}
	}
	return nil
}

// ownershipIssue applies §1.13-A to one entity's owner: the owner needs a
// record stating responsibilities and at least one decision it can make
// without sign-off, granted by someone, covering this entity. A declared
// liability is reported as a warning rather than hidden.
func ownershipIssue(e *Entity, owner string, rec *OwnershipRecord) (issue, bool) {
	is := issue{entity: e, pos: e.Annotations["owner"].Pos}
	is.suggestion = fmt.Sprintf("Add owners.%s: {model: [%s], responsibilities: [definitions, "+
		"data_quality, change_approval], decisions: [{name: approve_breaking_change, "+
		"without_signoff: true}], granted_by: <role>} to mml.yaml. If that authority cannot be "+
		"obtained, record ownership: liability so the gap is visible (§1.13).", owner, e.Name)
	if rec == nil {
		is.measured = fmt.Sprintf("%s is owned by %s, which has no ownership record", e.Name, owner)
		return is, true
	}
	if rec.Liability {
		is.level = SeverityWarning
		is.measured = fmt.Sprintf("%s is owned by %s, recorded as a liability: named without "+
			"the authority to act", e.Name, owner)
		return is, true
	}
	if gaps := recordGaps(e, rec); len(gaps) > 0 {
		is.measured = fmt.Sprintf("%s is owned by %s, but its record %s", e.Name, owner,
			strings.Join(gaps, "; "))
		return is, true
	}
	return issue{}, false
}

// recordGaps lists what an ownership record lacks for entity e.
func recordGaps(e *Entity, rec *OwnershipRecord) []string {
	var gaps []string
	if len(rec.Responsibilities) == 0 {
		gaps = append(gaps, "lists no responsibilities")
	}
	for _, r := range rec.Responsibilities {
		if !knownResponsibility(r) && len(strings.Fields(r)) < 3 {
			gaps = append(gaps, fmt.Sprintf("names responsibility %q, which is neither known "+
				"nor described in three words", r))
		}
	}
	if !slices.ContainsFunc(rec.Decisions, func(d Decision) bool { return d.WithoutSignoff }) {
		gaps = append(gaps, "lists no decision the owner can make without further sign-off")
	}
	if rec.GrantedBy == "" {
		gaps = append(gaps, "does not say who granted it (granted_by)")
	}
	if !slices.ContainsFunc(rec.Model, func(glob string) bool { return covers(glob, e) }) {
		gaps = append(gaps, "does not cover "+e.Name)
	}
	return gaps
}

func knownResponsibility(r string) bool {
	switch r {
	case "definitions", "data_quality", "change_approval", "access", "documentation",
		"incident_response":
		return true
	default:
		return false
	}
}

// covers reports whether an ownership glob matches e by name, canonical
// name or ID.
func covers(glob string, e *Entity) bool {
	for _, name := range []string{e.Name, e.Canonical, e.ID} {
		if ok, err := path.Match(glob, name); err == nil && ok {
			return true
		}
	}
	return false
}
