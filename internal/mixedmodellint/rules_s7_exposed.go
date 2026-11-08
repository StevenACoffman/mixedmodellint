package mixedmodellint

import (
	"fmt"
	"strings"
)

func section7ExposedDesigns() []design {
	return []design{
		{id: "7.6-A", title: "Agent-exposure contract", strength: Consider, check: check76A},
	}
}

// check76A reports an entity exposed to agents that does not tell them its
// grain and the coarser roll-ups to query instead (rule_plan.md §7.6-A).
// Each roll-up must be a time unit coarser than the grain's, or an entity
// that exists. A RAG chunk entity's grain must also state the chunk size and
// overlap. Comparing the declared roll-ups with the aggregates actually
// derived from the entity needs §7.5-A's lineage, so it waits for Phase 2.
func check76A(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		if v, ok := e.Annotations.Get("agent_exposed"); !ok || v == "false" {
			continue
		}
		if gaps := c.exposureGaps(e); len(gaps) > 0 {
			out = append(out, issue{
				entity: e,
				measured: fmt.Sprintf("%s is exposed to agents, but %s", e.Name,
					strings.Join(gaps, "; ")),
				suggestion: fmt.Sprintf("Give %s a grain (grain=\"one row per ...\") and "+
					"rollups=<time units coarser than the grain, or the aggregate entities that "+
					"serve them>, so agents answer from the coarsest entity that can (§7.6).",
					e.Name),
			})
		}
	}
	return out
}

// exposureGaps lists what e's agent-exposure contract lacks.
func (c *catalog) exposureGaps(e *Entity) []string {
	g, hasGrain := grainOf(e)
	rollups, hasRollups := e.Annotations.Get("rollups")
	var missing []string
	if !hasGrain {
		missing = append(missing, "grain")
	}
	if !hasRollups {
		missing = append(missing, "rollups")
	}
	var gaps []string
	if len(missing) > 0 {
		gaps = append(gaps, "it declares no "+orList(missing))
	}
	if c.rx.chunkName.MatchString(e.Name) && (!c.rx.chunkSize.MatchString(g.text) ||
		!strings.Contains(strings.ToLower(g.text), "overlap")) {
		gaps = append(gaps, "as a chunk entity its grain does not state the chunk size and "+
			"overlap (one row per 512-token chunk, 64 overlap)")
	}
	return append(gaps, c.rollupGaps(e, &g, rollups)...)
}

// rollupGaps lists the roll-ups that are no coarser than g's time unit, and
// those that are neither a time unit nor an entity.
func (c *catalog) rollupGaps(e *Entity, g *grainInfo, rollups string) []string {
	finest := 0
	for _, n := range g.nouns {
		finest = max(finest, timeUnitRank(n))
	}
	var notCoarser, unknown []string
	for _, r := range splitList(strings.Trim(rollups, "[]")) {
		switch rank := timeUnitRank(r); {
		case rank > 0 && rank <= finest:
			notCoarser = append(notCoarser, r)
		case rank == 0 && c.lookup(r, e.Source) == nil:
			unknown = append(unknown, r)
		}
	}
	var gaps []string
	if len(notCoarser) > 0 {
		gaps = append(gaps, "these roll-ups are not coarser than its grain: "+
			strings.Join(notCoarser, ", "))
	}
	if len(unknown) > 0 {
		gaps = append(gaps, "these roll-ups name neither a time unit nor an entity: "+
			strings.Join(unknown, ", "))
	}
	return gaps
}
