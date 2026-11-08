package mixedmodellint

import (
	"fmt"
	"strings"
)

// obtMinColumns is the width at which §3.3 treats an entity as a One Big
// Table whether or not its name or layer says so.
const obtMinColumns = 50

func section3LayerDesigns() []design {
	return []design{
		{
			id: "3.3-B", title: "Layer contract on wide tables", strength: Should,
			check: check33B,
		},
		{
			id: "3.5-A", title: "Foreign-schema mutation detector", strength: Should,
			check: check35A,
		},
	}
}

// check33B reports One Big Tables that do not say what they are built from
// (rule_plan.md §3.3-B): a table with at least obtMinColumns columns, an
// obt_ or wide_ name, or layer=obt must declare its layer, its grain and an
// upstream list of core entities. It checks the declaration, not the real
// lineage; a view's query would show that (Design A, Phase 2).
func check33B(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		if foreignSchema(e) || !c.oneBigTable(e) {
			continue
		}
		if gaps := c.obtContractGaps(e); len(gaps) > 0 {
			out = append(out, issue{
				entity: e,
				measured: fmt.Sprintf("wide table %s (%d columns) %s", e.Name, len(e.Fields),
					strings.Join(gaps, "; ")),
				suggestion: fmt.Sprintf("Annotate %s with layer=obt, its grain and "+
					"upstream=<the core entities it is built from>, or reduce it to a modeled "+
					"entity (§3.3).", e.Name),
			})
		}
	}
	return out
}

// oneBigTable reports whether e is an OBT by its width, name or layer.
func (c *catalog) oneBigTable(e *Entity) bool {
	layer, _ := e.Annotations.Get("layer")
	return len(e.Fields) >= obtMinColumns || layer == "obt" || c.rx.obtName.MatchString(e.Name)
}

// obtContractGaps lists what e's layer contract lacks: a layer, a grain, or
// an upstream list naming only core entities the catalog has.
func (c *catalog) obtContractGaps(e *Entity) []string {
	var gaps, missing []string
	for _, key := range []string{"layer", "grain", "upstream"} {
		if !e.Annotations.Has(key) {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		gaps = append(gaps, "declares no "+orList(missing))
	}
	upstream, _ := e.Annotations.Get("upstream")
	var unknown, notCore []string
	for _, name := range splitList(strings.Trim(upstream, "[]")) {
		u := c.lookup(name, e.Source)
		layer := ""
		if u != nil {
			layer, _ = u.Annotations.Get("layer")
		}
		switch {
		case u == nil:
			unknown = append(unknown, name)
		case layer != "core":
			notCore = append(notCore, name)
		}
	}
	if len(unknown) > 0 {
		gaps = append(gaps, "its upstream names unknown entities: "+strings.Join(unknown, ", "))
	}
	if len(notCore) > 0 {
		gaps = append(gaps, "its upstream names entities that are not layer=core: "+
			strings.Join(notCore, ", "))
	}
	return gaps
}

// check35A reports DDL that changes a table someone else controls, one
// declared source=legacy or source=third_party (rule_plan.md §3.5-A): an
// ALTER, RENAME, CREATE INDEX or CREATE TRIGGER on it. A statement in the
// file that creates the table is part of the copy of the vendor's schema the
// repo keeps, not a change to it, so it is not reported. A DROP removes the
// entity and its changes with it, so it is not reported either.
func check35A(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		if !foreignSchema(e) {
			continue
		}
		src, _ := e.Annotations.Get("source")
		for _, ch := range e.Changes {
			if ch.Pos.File == e.Pos.File {
				continue
			}
			out = append(out, issue{
				entity: e, pos: ch.Pos,
				measured: fmt.Sprintf("%s runs %s on %s, a %s table this team does not control",
					ch.Pos.File, ch.Statement, e.Name, strings.ReplaceAll(src, "_", "-")),
				suggestion: fmt.Sprintf("Leave %s untouched: put extra columns in %s_ext in a "+
					"schema you own, keyed by its id, or build what you need downstream in "+
					"staging; waive this with a reason for a vendor-sanctioned extension "+
					"point (§3.5).", e.Name, e.Name),
			})
		}
	}
	return out
}
