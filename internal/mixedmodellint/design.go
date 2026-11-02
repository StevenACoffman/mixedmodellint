package mixedmodellint

import (
	"cmp"
	"strings"
)

// design is one implementation of one rule, as rule_plan.md describes it. The
// ID's prefix before "-" is the rule: "7.1-B" checks §7.1.
type design struct {
	id        string
	title     string
	strength  Strength
	heuristic bool
	check     func(c *catalog) []issue
}

// DesignInfo describes one implemented design for commands that list or
// explain designs: its rule_plan.md ID, title, and the severity its findings
// get by default.
type DesignInfo struct {
	ID       string
	Title    string
	Severity Severity
}

// issue is what a design reports. The engine turns it into a Finding,
// filling in the rule, design, title and severity, so no design can get its
// own severity wrong.
type issue struct {
	// level overrides the design's severity for designs whose rule_plan text
	// grades findings, such as 5.1-A (a reserved word is an error, a generic
	// name a warning). Zero means "use the design's severity".
	level      Severity
	entity     *Entity
	field      string
	pos        Position
	measured   string
	suggestion string
}

// catalog is the linked IR every design reads: all entities from all
// sources, indexed by canonical name, plus the query corpus.
type catalog struct {
	entities    []*Entity
	byCanonical map[string][]*Entity
	queries     []Query
	rx          *lexicon
	artifacts   Artifacts
	// sidecar holds mml.yaml's annotations, the only place an entity that no
	// longer exists, such as a dropped table, can declare anything.
	sidecar Sidecar
	// previous is the model the change is compared with, from mml.lock.json;
	// nil when there is none, and the designs that need it report nothing.
	previous []*Entity
}

func newCatalog(entities []*Entity, queries []Query, rx *lexicon) *catalog {
	c := &catalog{
		entities:    entities,
		byCanonical: map[string][]*Entity{},
		queries:     queries,
		rx:          rx,
	}
	for _, e := range entities {
		c.byCanonical[e.Canonical] = append(c.byCanonical[e.Canonical], e)
	}
	return c
}

// lookup returns the entity a name refers to, preferring one of the same
// source kind as from (a SQL foreign key names a SQL table) and otherwise the
// first linked entity. It returns nil when nothing has that canonical name.
func (c *catalog) lookup(name string, from SourceKind) *Entity {
	// A schema-qualified SQL name links on its last part.
	candidates := c.byCanonical[Canonical(lastPart(name))]
	for _, e := range candidates {
		if e.Source == from {
			return e
		}
	}
	if len(candidates) > 0 {
		return candidates[0]
	}
	return nil
}

// parents returns the entities e references, each once, in reference order.
func (c *catalog) parents(e *Entity) []*Entity {
	var out []*Entity
	seen := map[*Entity]bool{e: true}
	for _, r := range e.Refs {
		p := c.lookup(r.Target, e.Source)
		if p == nil || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// severityFor derives a design's severity from its rule's strength, one level
// lower when the design is heuristic (rule_plan.md §3). A heuristic CONSIDER
// design stays a note rather than vanishing.
func severityFor(d design) Severity {
	var s Severity
	switch d.strength {
	case Must:
		s = SeverityError
	case Should:
		s = SeverityWarning
	default:
		s = SeverityNote
	}
	if d.heuristic && s > SeverityNote {
		s--
	}
	return s
}

// run executes every design over c and returns the findings, unsorted.
func run(c *catalog, designs []design) []Finding {
	var out []Finding
	for _, d := range designs {
		rule, _, _ := strings.Cut(d.id, "-")
		sev := severityFor(d)
		for _, is := range d.check(c) {
			f := Finding{
				Rule:       rule,
				Design:     d.id,
				Title:      d.title,
				Severity:   cmp.Or(is.level, sev),
				Field:      is.field,
				Pos:        is.pos,
				Measured:   is.measured,
				Suggestion: is.suggestion,
			}
			if is.entity != nil {
				f.EntityID = is.entity.ID
				f.Entity = is.entity.Name
				if f.Pos == (Position{}) {
					f.Pos = is.entity.Pos
				}
			}
			out = append(out, f)
		}
	}
	return out
}

// allDesigns lists every implemented design. It is a function, not a
// package variable, so there is no global state (summary_rules §1).
func allDesigns() []design {
	var ds []design
	ds = append(ds, section1Designs()...)
	ds = append(ds, section3Designs()...)
	ds = append(ds, section4Designs()...)
	ds = append(ds, section5Designs()...)
	ds = append(ds, section6Designs()...)
	ds = append(ds, section7Designs()...)
	ds = append(ds, section7QueryDesigns()...)
	ds = append(ds, section8Designs()...)
	ds = append(ds, section9Designs()...)
	return ds
}

// Designs lists every implemented design, in the order they run.
func Designs() []DesignInfo {
	ds := allDesigns()
	out := make([]DesignInfo, 0, len(ds))
	for _, d := range ds {
		out = append(out, DesignInfo{ID: d.id, Title: d.title, Severity: severityFor(d)})
	}
	return out
}
