package mixedmodellint

import (
	"fmt"
	"strings"
)

func section8MetricDesigns() []design {
	return []design{
		{
			id: "8.8-A", title: "Non-averageable domain marking", strength: Should,
			check: check88A,
		},
		{
			id: "8.9-B", title: "Reconciliation contract annotation", strength: Should,
			check: check89B,
		},
	}
}

// check88A reports values whose average is meaningless declared or stored
// as if it were not (rule_plan.md §8.8-A): an identifier or code declared a
// measure, and an embedding that does not say how it may be pooled. A key,
// a reference, an allowed list or a UUID is nominal for certain; a name
// alone (zip_code) is a note. scale=ordinal or interval marks a coded scale,
// such as a 1-5 rating, that may be averaged.
func check88A(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		if outsideConventions(e) {
			continue
		}
		for i := range e.Fields {
			f := &e.Fields[i]
			if is, ok := nominalMeasure(e, f); ok {
				out = append(out, is)
			}
			if is, ok := unpooledEmbedding(e, f); ok {
				out = append(out, is)
			}
		}
	}
	return out
}

// nominalMeasure reports f when it is nominal yet declared a measure.
func nominalMeasure(e *Entity, f *Field) (issue, bool) {
	class, _ := f.Annotations.Get("measure")
	class, _, _ = strings.Cut(class, " ")
	if class != "additive" && class != "semi_additive" && class != "non_additive" ||
		f.Annotations.Has("scale") {
		return issue{}, false
	}
	is := issue{
		entity: e, field: f.Name, pos: f.Pos,
		suggestion: fmt.Sprintf("Store %s as text so arithmetic cannot apply, or declare "+
			"measure=nominal; if it is a coded scale that may be averaged, declare "+
			"scale=ordinal or scale=interval (§8.8).", f.Name),
	}
	switch {
	case isKeyOrRef(e, f.Name) || len(enumValues(e, f)) > 0 || f.Type == TypeUUID:
		is.measured = fmt.Sprintf("%s.%s identifies or codes something, so its average is "+
			"meaningless, but it is declared measure=%s", e.Name, f.Name, class)
		is.suggestion = fmt.Sprintf("Remove the measure declaration from %s, or declare "+
			"measure=nominal; if it is a coded scale that may be averaged, declare "+
			"scale=ordinal or scale=interval (§8.8).", f.Name)
	case nominalName(f.Name):
		is.level = SeverityNote
		is.measured = fmt.Sprintf("%s.%s is named like an identifier or code, so its average "+
			"is likely meaningless, but it is declared measure=%s", e.Name, f.Name, class)
	default:
		return issue{}, false
	}
	return is, true
}

// unpooledEmbedding reports an embedding with no pooling declaration: the
// mean of embeddings from different topics points at none of them.
func unpooledEmbedding(e *Entity, f *Field) (issue, bool) {
	if !f.Annotations.Has("embedding") && !strings.HasPrefix(strings.ToLower(f.RawType), "vector") {
		return issue{}, false
	}
	pooling, _ := f.Annotations.Get("pooling")
	if pooling == "none" ||
		strings.HasPrefix(pooling, "same_topic(") && strings.HasSuffix(pooling, ")") {
		return issue{}, false
	}
	return issue{
		entity: e, field: f.Name, pos: f.Pos,
		measured: fmt.Sprintf("%s.%s is an embedding with no valid pooling declaration, so "+
			"nothing stops it being averaged across topics", e.Name, f.Name),
		suggestion: "Declare pooling=none, or pooling=same_topic(<cluster column>) if vectors " +
			"may be averaged within one cluster (§8.8).",
	}, true
}

// check89B reports key metrics that do not say what they reconcile with
// (rule_plan.md §8.9-B): each measure on an entity declared key_metric, and
// each field declared key_metric, must name reconciles_with=<entity>.<field>,
// an additive measure on another entity in compatible units. Whether the
// target is upstream, at a finer grain, needs lineage and waits for Phase 2.
func check89B(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		keyEntity := e.Annotations.Has("key_metric") && annotationTrue(e.Annotations, "key_metric")
		for i := range e.Fields {
			f := &e.Fields[i]
			if !annotationTrue(f.Annotations, "key_metric") &&
				(!keyEntity || !f.Annotations.Has("measure")) {
				continue
			}
			if problem := c.reconciliationProblem(e, f); problem != "" {
				out = append(out, issue{
					entity: e, field: f.Name, pos: f.Pos,
					measured: fmt.Sprintf("%s.%s is a key metric, but %s", e.Name, f.Name,
						problem),
					suggestion: fmt.Sprintf("Declare reconciles_with=<atomic entity>.<additive "+
						"measure> on %s.%s, with tolerance= if they may differ, so CI can check "+
						"that both ways of computing it agree (§8.9).", e.Name, f.Name),
				})
			}
		}
	}
	return out
}

// annotationTrue reports whether key is declared and not "false".
func annotationTrue(a Annotations, key string) bool {
	v, ok := a.Get(key)
	return ok && v != "false"
}

// reconciliationProblem returns what f's reconciles_with declaration lacks,
// or "".
func (c *catalog) reconciliationProblem(e *Entity, f *Field) string {
	ref, ok := f.Annotations.Get("reconciles_with")
	if !ok {
		return "it declares no reconciles_with"
	}
	ref, _ = splitParams(ref)
	dot := strings.LastIndex(ref, ".")
	t := c.lookup(ref[:max(dot, 0)], e.Source)
	if dot < 0 || t == nil {
		return fmt.Sprintf("its reconciles_with=%s names no entity", ref)
	}
	tf, ok := t.Field(ref[dot+1:])
	if !ok {
		return fmt.Sprintf("its reconciles_with=%s names no field of %s", ref, t.Name)
	}
	switch class, _ := tf.Annotations.Get("measure"); {
	case t == e:
		return "it reconciles with its own entity, which checks nothing"
	case !strings.HasPrefix(class, "additive"):
		return fmt.Sprintf("%s is not declared measure=additive, so sums cannot be compared", ref)
	}
	return unitMismatch(f, tf, ref)
}

// unitMismatch says how f's unit or currency differs from that of tf, which
// ref names, or "" when neither declared one differs.
func unitMismatch(f, tf *Field, ref string) string {
	for _, key := range []string{"unit", "currency"} {
		a, aok := f.Annotations.Get(key)
		b, bok := tf.Annotations.Get(key)
		if aok && bok && !strings.EqualFold(a, b) {
			return fmt.Sprintf("its %s, %s, differs from that of %s, %s", key, a, ref, b)
		}
	}
	return ""
}
