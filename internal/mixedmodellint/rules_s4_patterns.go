package mixedmodellint

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// Thresholds of §4.11: an entity repeats a cluster when it fills
// minClusterCoverage of its slots; minClusterEntities such entities make a
// group worth a pattern check, since two sharing contact fields is common
// and fine; an entity whose fields changed by refactorShare is redesigned
// and needs its pattern check again.
const (
	minClusterCoverage = 0.6
	minClusterEntities = 3
	refactorShare      = 0.3
)

// clusterGroup is the entities that each repeat one cluster without
// referencing an entity that holds it.
type clusterGroup struct {
	cluster *patternCluster
	members []*Entity
}

// patternRecord is a parsed pattern annotation: `party_role=rejected
// reason="..." members=4` or `none-applicable reason="..."`.
type patternRecord struct {
	name, decision, reason, fp string
	members                    int
}

func section4PatternDesigns() []design {
	return []design{
		{
			id: "4.11-A", title: "Repeated attribute cluster across sibling entities",
			strength: Should, check: check411A,
		},
		{
			id: "4.11-B", title: "Fingerprint-bound pattern-evaluation record", strength: Should,
			check: check411B,
		},
	}
}

// clusterGroups returns, for each cluster, the entities that repeat it,
// when there are at least minClusterEntities of them.
func (c *catalog) clusterGroups() []clusterGroup {
	var out []clusterGroup
	for i := range c.rx.clusters {
		cl := &c.rx.clusters[i]
		g := clusterGroup{cluster: cl}
		for _, e := range c.entities {
			if e.Kind != KindView && !foreignSchema(e) && clusterCoverage(e, cl) >=
				minClusterCoverage && !c.holdsClusterElsewhere(e, cl) {
				g.members = append(g.members, e)
			}
		}
		if len(g.members) >= minClusterEntities {
			out = append(out, g)
		}
	}
	return out
}

// clusterCoverage returns the share of cl's slots e's fields fill, reading
// vendor_email as email on vendors.
func clusterCoverage(e *Entity, cl *patternCluster) float64 {
	stems := map[string]bool{}
	for i := range e.Fields {
		name := snakeName(e.Fields[i].Name)
		stems[strings.TrimPrefix(name, e.Canonical+"_")] = true
	}
	filled := 0
	for _, slot := range cl.slots {
		if slices.ContainsFunc(slot, func(s string) bool { return stems[s] }) {
			filled++
		}
	}
	return float64(filled) / float64(len(cl.slots))
}

// holdsClusterElsewhere reports whether e references an entity that holds
// cl for everyone, such as a party or location, so its own copy is
// incidental.
func (c *catalog) holdsClusterElsewhere(e *Entity, cl *patternCluster) bool {
	return slices.ContainsFunc(c.parents(e), func(p *Entity) bool {
		return slices.Contains(cl.shared, p.Canonical)
	})
}

// check411A reports a cluster of attributes, such as name, email, phone and
// address, repeated across at least minClusterEntities entities that share
// no entity holding it, before any of them records a pattern decision
// (rule_plan.md §4.11-A). The rule asks for the pattern to be weighed, not
// adopted, so a decision either way on any member passes; 4.11-B checks
// each record.
func check411A(c *catalog) []issue {
	var out []issue
	for _, g := range c.clusterGroups() {
		if slices.ContainsFunc(g.members, func(e *Entity) bool {
			return e.Annotations.Has("pattern") || e.Annotations.Has("pattern_adr")
		}) {
			continue
		}
		names := make([]string, len(g.members))
		for i, e := range g.members {
			names[i] = e.Name
		}
		out = append(out, issue{
			entity: g.members[0],
			measured: fmt.Sprintf("%d entities (%s) each repeat the %s attributes (%s)",
				len(g.members), strings.Join(names, ", "), g.cluster.name, slotNames(g.cluster)),
			suggestion: fmt.Sprintf("Before adding another, evaluate the %s pattern: %s. Weigh "+
				"its cost (self-joins, generic names stakeholders do not use), then record the "+
				"decision on each entity with pattern=%s=adopted|rejected reason=\"...\" (§4.11).",
				g.cluster.pattern, patternSketch(g.cluster), g.cluster.pattern),
		})
	}
	return out
}

// slotNames lists a cluster's slots by their first stem.
func slotNames(cl *patternCluster) string {
	names := make([]string, len(cl.slots))
	for i, slot := range cl.slots {
		names[i] = slot[0]
	}
	return strings.Join(names, ", ")
}

// patternSketch says what adopting the pattern for cl looks like.
func patternSketch(cl *patternCluster) string {
	if cl.pattern == "party_role" {
		return "a party(party_id, party_type) with party_role(party_id, role) and " +
			"contact_method(party_id, kind, value), each entity keyed by party_id"
	}
	return fmt.Sprintf("one %s entity holding these attributes, referenced by %s_id",
		cl.shared[0], cl.shared[0])
}

// check411B reports entities that need a pattern decision and lack a valid,
// current one (rule_plan.md §4.11-B): every member of a 4.11-A cluster
// group, and, against mml.lock.json, every entity that is new or whose
// fields changed by refactorShare. A decision must name a known pattern and
// give a reason unless it adopts it; an adoption must reference the
// pattern's shared entity (M2); and a rejection goes stale when its cluster
// group has grown past the members= it was weighed for, or its fp= no
// longer matches the entity (M1).
func check411B(c *catalog) []issue {
	groupOf := map[*Entity]*clusterGroup{}
	groups := c.clusterGroups()
	for i := range groups {
		for _, e := range groups[i].members {
			groupOf[e] = &groups[i]
		}
	}
	var out []issue
	for _, e := range c.entities {
		g, member := groupOf[e]
		if !member && !c.redesigned(e) {
			continue
		}
		if problem := c.patternProblem(e, g); problem != "" {
			out = append(out, patternIssue(e, g, problem))
		}
	}
	return out
}

// redesigned reports whether e is new since the previous model or had at
// least refactorShare of its fields added or removed. Without a previous
// model nothing is.
func (c *catalog) redesigned(e *Entity) bool {
	if c.previous == nil || e.Kind == KindView || foreignSchema(e) {
		return false
	}
	old, ok := c.previousOf(e.ID)
	if !ok {
		return true
	}
	changed := 0
	for i := range e.Fields {
		if _, kept := old.Field(e.Fields[i].Name); !kept {
			changed++
		}
	}
	for i := range old.Fields {
		if _, kept := e.Field(old.Fields[i].Name); !kept {
			changed++
		}
	}
	return float64(changed) >= refactorShare*float64(max(len(old.Fields), 1))
}

// patternProblem returns what e's pattern decision lacks, or "" when it is
// valid and current. g is e's cluster group, or nil.
func (c *catalog) patternProblem(e *Entity, g *clusterGroup) string {
	v, ok := e.Annotations.Get("pattern")
	if !ok {
		return c.patternADRProblem(e)
	}
	rec, problem := c.parsePattern(v)
	switch {
	case problem != "":
		return problem
	case rec.decision != "adopted" && rec.reason == "":
		return fmt.Sprintf("its %s decision gives no reason", rec.decision)
	default:
		return c.patternDrift(e, g, &rec)
	}
}

// patternDrift returns how e's structure has moved away from a readable
// decision rec: an adoption e does not follow (M2), or a decision weighed
// for a smaller cluster group or another shape (M1); "" when it has not.
func (c *catalog) patternDrift(e *Entity, g *clusterGroup, rec *patternRecord) string {
	switch {
	case rec.followsPattern() && g != nil && !c.holdsClusterElsewhere(e, g.cluster):
		return fmt.Sprintf("it records %s=%s but references none of %s", rec.name,
			rec.decision, orList(g.cluster.shared))
	case g != nil && rec.members > 0 && len(g.members) > rec.members:
		return fmt.Sprintf("its decision was weighed when there were %d", rec.members)
	case rec.fp != "" && rec.fp != shapeFingerprint(e):
		return fmt.Sprintf("its decision's fp=%s no longer matches %s (now %s)", rec.fp, e.Name,
			shapeFingerprint(e))
	default:
		return ""
	}
}

// followsPattern reports whether the decision is to use the pattern, as
// adopted or adapted, which the structure must then show.
func (r *patternRecord) followsPattern() bool {
	return r.decision == "adopted" || r.decision == "adapted"
}

// patternADRProblem checks a pattern_adr record, the alternative to a
// pattern annotation: an ADR that exists and names a known pattern.
func (c *catalog) patternADRProblem(e *Entity) string {
	ref, ok := e.Annotations.Get("pattern_adr")
	if !ok {
		return "it records no pattern decision"
	}
	adr, ok := c.adrAt(ref)
	if !ok {
		return fmt.Sprintf("its pattern_adr %q names no ADR in docs/adr/", ref)
	}
	words := splitWords(adr.Text)
	for p := range c.rx.patterns {
		if containsRun(words, strings.Split(p, "_")) {
			return ""
		}
	}
	return fmt.Sprintf("its ADR %s names no pattern", adr.Path)
}

// parsePattern reads a pattern annotation, returning what makes it
// unreadable, if anything.
func (c *catalog) parsePattern(v string) (patternRecord, string) {
	text, params := splitParams(v)
	rec := patternRecord{reason: params["reason"], fp: params["fp"]}
	rec.members, _ = strconv.Atoi(params["members"])
	if strings.TrimSpace(text) == "none-applicable" {
		rec.decision = "none-applicable"
		return rec, ""
	}
	for k, d := range params {
		switch d {
		case "adopted", "adapted", "rejected":
			if !c.rx.patterns[k] {
				return rec, fmt.Sprintf("its pattern %q is not one of %s", k,
					strings.Join(slices.Sorted(maps.Keys(c.rx.patterns)), ", "))
			}
			rec.name, rec.decision = k, d
			return rec, ""
		}
	}
	return rec, fmt.Sprintf("its pattern record %q names no <pattern>=adopted|adapted|rejected "+
		"or none-applicable", v)
}

// patternIssue reports e's missing or invalid pattern decision. For a
// cluster member the suggested record carries members=, so it goes stale
// when the group grows.
func patternIssue(e *Entity, g *clusterGroup, problem string) issue {
	why := "is new or was redesigned since mml.lock.json"
	fix := "Weigh whether one of the patterns (account, agreement, event, location, " +
		"party_role, product, classification) fits it, and record " +
		"pattern=<name>=adopted|adapted|rejected reason=\"...\""
	if g != nil {
		why = fmt.Sprintf("is one of %d entities repeating the %s attributes", len(g.members),
			g.cluster.name)
		fix = fmt.Sprintf("Weigh whether the %s pattern fits it now, and record "+
			"pattern=%s=adopted|adapted|rejected reason=\"...\" members=%d", g.cluster.pattern,
			g.cluster.pattern, len(g.members))
	}
	return issue{
		entity: e, pos: e.Annotations["pattern"].Pos,
		measured:   fmt.Sprintf("%s %s, but %s", e.Name, why, problem),
		suggestion: fix + ", or pattern=none-applicable reason=\"...\" (§4.11).",
	}
}
