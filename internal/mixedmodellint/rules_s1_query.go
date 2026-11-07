package mixedmodellint

import (
	"fmt"
	"slices"
	"strings"
)

// check15D applies §1.5-D: every column pair a join equates between two
// stored entities must be a modeled relationship, or the join is
// "plausible on the wrong key" (order_items.return_id = orders.order_id).
//
// A pair is sanctioned when it follows a reference of either side, declared
// or inferred from a name (6.2-A already reports an undeclared one), or
// when both columns reference the same entity by the same name, a join
// through a shared dimension. An entity declared role=identity_map is
// exempt, since matching on non-key attributes is its purpose.
//
// Deviation: rule_plan.md also sanctions relationships in
// model/conceptual.yaml and exempts joins carrying a -- mml:join comment;
// neither is read yet, so such a join needs a waive on the entity.
func check15D(c *catalog) []issue {
	var out []issue
	for i := range c.queries {
		q := &c.queries[i]
		s := c.scopeOf(q)
		for _, j := range q.Joins {
			for _, p := range j.On {
				le, lf := s.resolve(p.Left)
				re, rf := s.resolve(p.Right)
				if lf == nil || rf == nil || le == re || identityMap(le) || identityMap(re) ||
					c.sanctioned(le, lf, re, rf) {
					continue
				}
				out = append(out, issue{
					entity: le, field: lf.Name, pos: j.Pos,
					measured: fmt.Sprintf("the join %s.%s = %s.%s follows no reference between "+
						"%s and %s", le.Name, lf.Name, re.Name, rf.Name, le.Name, re.Name),
					suggestion: fmt.Sprintf("%s Check the grain (§7.2) and correct the key, or "+
						"declare the reference if it is real (§1.5).", c.modeledJoin(le, re)),
				})
			}
		}
	}
	return out
}

// identityMap reports whether e is declared an identity map (§4.2).
func identityMap(e *Entity) bool {
	role, _ := e.Annotations.Get("role")
	return role == "identity_map"
}

// sanctioned reports whether a.af = b.bf follows a modeled relationship.
func (c *catalog) sanctioned(a *Entity, af *Field, b *Entity, bf *Field) bool {
	if c.follows(a, af, b, bf) || c.follows(b, bf, a, af) {
		return true
	}
	// A shared dimension: both columns, of the same name, reference one
	// entity.
	if !strings.EqualFold(af.Name, bf.Name) {
		return false
	}
	at, bt := c.referenced(a, af), c.referenced(b, bf)
	return at != nil && at == bt
}

// follows reports whether from.ff is a reference to to.tf: a reference of
// from covering ff whose target is to, at tf or, when it names no target
// columns, at to's primary key.
func (c *catalog) follows(from *Entity, ff *Field, to *Entity, tf *Field) bool {
	return slices.ContainsFunc(from.Refs, func(r Ref) bool {
		i := slices.IndexFunc(
			r.Fields,
			func(f string) bool { return strings.EqualFold(f, ff.Name) },
		)
		if i < 0 || c.lookup(r.Target, from.Source) != to {
			return false
		}
		if i < len(r.TargetFields) {
			return strings.EqualFold(r.TargetFields[i], tf.Name)
		}
		return containsFold(to.PrimaryKey, tf.Name)
	})
}

// referenced returns the entity e.f references, or nil.
func (c *catalog) referenced(e *Entity, f *Field) *Entity {
	for _, r := range e.Refs {
		if containsFold(r.Fields, f.Name) {
			return c.lookup(r.Target, e.Source)
		}
	}
	return nil
}

// modeledJoin states the join the model does declare between a and b, or
// that it declares none.
func (c *catalog) modeledJoin(a, b *Entity) string {
	for _, pair := range [][2]*Entity{{a, b}, {b, a}} {
		from, to := pair[0], pair[1]
		for _, r := range from.Refs {
			if c.lookup(r.Target, from.Source) != to || len(r.Fields) == 0 {
				continue
			}
			target := r.TargetFields
			if len(target) == 0 {
				target = to.PrimaryKey
			}
			return fmt.Sprintf("The modeled join is %s = %s.", columnList(from, r.Fields),
				columnList(to, target))
		}
	}
	return fmt.Sprintf("No reference links %s and %s.", a.Name, b.Name)
}

// columnList writes e's columns as orders.order_id, or as
// order_lines.(order_id, line_no) for more than one.
func columnList(e *Entity, cols []string) string {
	if len(cols) == 1 {
		return e.Name + "." + cols[0]
	}
	return fmt.Sprintf("%s.(%s)", e.Name, strings.Join(cols, ", "))
}
