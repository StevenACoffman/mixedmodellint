package mixedmodellint

import (
	"fmt"
	"slices"
	"strings"
)

func section2OpsDesigns() []design {
	return []design{
		{
			id: "2.8-A", title: "State machine and permissions as data", strength: Consider,
			check: check28A,
		},
	}
}

// check28A reports entities agents may change whose permitted operations or
// state transitions are not written down as data an agent can check (§2.8),
// and declarations the database grants contradict (M2).
func check28A(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		via := c.agentAccess(e)
		if via == "" || foreignSchema(e) {
			continue
		}
		if problems := c.agentContractProblems(e); len(problems) > 0 {
			out = append(out, issue{
				entity: e,
				measured: fmt.Sprintf("agents may change %s (%s), but %s", e.Name, via,
					strings.Join(problems, "; ")),
				suggestion: fmt.Sprintf("Declare the operations agents may perform "+
					"(agent_ops=read,write,...), matching what the database grants them, and "+
					"encode each status field's allowed moves as data, e.g. a table "+
					"%s_status_transitions(from_state, to_state, policy) or a transitions "+
					"annotation (§2.8).", e.Canonical),
			})
		}
	}
	return out
}

// agentAccess says how agents may change e, or "" when they may not: its
// agent_ops allows more than reading, it is declared agent_exposed, or the
// database grants an agent role a write.
func (c *catalog) agentAccess(e *Entity) string {
	if ops, ok := agentOps(e); ok && agentMayWrite(ops) {
		return "agent_ops=" + strings.Join(ops, ",")
	}
	if v, ok := e.Annotations.Get("agent_exposed"); ok && v != "false" {
		return "agent_exposed"
	}
	for _, g := range c.agentGrants(e) {
		if writes := writePrivileges(g.Privileges); len(writes) > 0 {
			return fmt.Sprintf("%s is granted %s", g.Role, strings.Join(writes, ", "))
		}
	}
	return ""
}

// agentContractProblems lists what e's agent contract lacks or gets wrong.
func (c *catalog) agentContractProblems(e *Entity) []string {
	var problems []string
	ops, declared := agentOps(e)
	if !declared {
		problems = append(problems, "it declares no agent_ops")
	}
	for i := range e.Fields {
		f := &e.Fields[i]
		if c.rx.statusField.MatchString(f.Name) && !c.transitionsEncoded(e, f) {
			problems = append(problems, f.Name+" has no transition encoding")
		}
	}
	if !declared {
		return problems
	}
	for _, g := range c.agentGrants(e) {
		for _, p := range writePrivileges(g.Privileges) {
			op := map[string]string{"INSERT": "write", "UPDATE": "write", "DELETE": "delete"}[p]
			if !slices.Contains(ops, op) {
				problems = append(problems, fmt.Sprintf("agent_ops omits %s, yet %s is granted %s",
					op, g.Role, p))
			}
		}
	}
	return problems
}

// writePrivileges returns the privileges among privs that change rows.
func writePrivileges(privs []string) []string {
	return slices.DeleteFunc(slices.Clone(privs), func(p string) bool {
		return p != "INSERT" && p != "UPDATE" && p != "DELETE"
	})
}

// transitionsEncoded reports whether f's allowed moves are data: declared on
// the field, kept in a {entity}_{field}_transitions or {entity}_transitions
// table, or in a generic state_transitions table that references e.
func (c *catalog) transitionsEncoded(e *Entity, f *Field) bool {
	if f.Annotations.Has("transitions") {
		return true
	}
	for _, name := range []string{
		e.Canonical + "_" + Canonical(f.Name) + "_transitions", e.Canonical + "_transitions",
	} {
		if len(c.byCanonical[Canonical(name)]) > 0 {
			return true
		}
	}
	for _, t := range c.byCanonical["state_transition"] {
		if slices.Contains(c.parents(t), e) {
			return true
		}
	}
	return false
}
