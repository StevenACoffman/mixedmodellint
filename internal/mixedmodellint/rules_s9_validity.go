package mixedmodellint

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
)

// period is how an entity records when each row held on one time axis: a
// from/to pair of fields, or one range column holding both ends.
type period struct {
	from, to *Field
	span     *Field
}

// timeAxis is one of the two times a row can be valid in: valid time, when
// the fact was true in the world (§9.5), or transaction time, when the system
// believed it (§9.6). A field declaring the time= role wins over a field
// only named for it.
type timeAxis struct {
	fromRole, toRole   string
	fromNames, toNames []string
}

func validAxis() timeAxis {
	return timeAxis{
		fromRole: "valid_from", toRole: "valid_to",
		fromNames: []string{"valid_from", "effective_from"},
		toNames:   []string{"valid_to", "effective_to"},
	}
}

func txnAxis() timeAxis {
	return timeAxis{
		fromRole: "txn_from", toRole: "txn_to",
		fromNames: []string{"txn_from", "system_from", "sys_from"},
		toNames:   []string{"txn_to", "system_to", "sys_to"},
	}
}

func section9ValidityDesigns() []design {
	return []design{
		{id: "9.5-A", title: "Lifespan-fact shape check", strength: Should, check: check95A},
		{
			id: "9.6-A", title: "Declared-requirement shape check", strength: Should,
			check: check96A,
		},
	}
}

func (p period) complete() bool { return p.span != nil || (p.from != nil && p.to != nil) }

// periodsOf returns e's period on each axis, in order. A range column
// completes the first axis still missing an end, so two range columns serve
// a bitemporal entity's two axes.
func periodsOf(e *Entity, axes ...timeAxis) []period {
	out := make([]period, len(axes))
	for i, a := range axes {
		out[i] = period{
			from: roleField(e, a.fromRole, a.fromNames),
			to:   roleField(e, a.toRole, a.toNames),
		}
	}
	spans := rangeFields(e)
	for i := range out {
		if !out[i].complete() && len(spans) > 0 {
			out[i].span, spans = spans[0], spans[1:]
		}
	}
	return out
}

func roleField(e *Entity, role string, names []string) *Field {
	for i := range e.Fields {
		if t, _ := e.Fields[i].Annotations.Get("time"); t == role {
			return &e.Fields[i]
		}
	}
	for _, n := range names {
		if f, ok := e.Field(n); ok {
			return f
		}
	}
	return nil
}

// rangeFields returns e's range columns (tstzrange, tsrange, daterange),
// each of which holds a whole period with its bounds.
func rangeFields(e *Entity) []*Field {
	var out []*Field
	for i := range e.Fields {
		switch strings.ToLower(e.Fields[i].RawType) {
		case "tstzrange", "tsrange", "daterange":
			out = append(out, &e.Fields[i])
		}
	}
	return out
}

// check95A reports a fact with a lifespan whose shape cannot answer "which
// version held at time X" (rule_plan.md §9.5-A): it needs valid_from and
// valid_to of one type, a NOT NULL start and, in SQL, a CHECK tying the two
// together, or one range column. It also reports the minority when the
// model marks open ends both with NULL and with a far-future sentinel, since
// each convention needs its own query form. The end= and null=open
// declarations on the pair are §9.3-B's.
func check95A(c *catalog) []issue {
	var out []issue
	var facts []*Entity
	for _, e := range c.entities {
		why, ok := lifespanReason(e)
		if !ok {
			continue
		}
		facts = append(facts, e)
		if gaps := lifespanGaps(e, periodsOf(e, validAxis())[0]); len(gaps) > 0 {
			out = append(out, lifespanIssue(e, why, gaps))
		}
	}
	return append(out, openEndIssues(facts)...)
}

// lifespanReason says why e is taken for a fact with a lifespan: lifespan=
// declares it; otherwise its name or its key does. Only records kept on
// their own qualify.
func lifespanReason(e *Entity) (string, bool) {
	if outsideConventions(e) || !storedRecord(e) {
		return "", false
	}
	if v, ok := e.Annotations.Get("lifespan"); ok {
		return "it declares lifespan=" + v, v != "false" && v != "none"
	}
	if why, ok := lifespanName(splitWords(e.Name)); ok {
		return why, true
	}
	return lifespanKey(e)
}

// lifespanName reports a name with a word for something that is replaced
// over time (price, plan) or a last word for kept versions (history,
// versions). Whole words are compared, so explanations is not a plan. A dim_
// table is an SCD1 dimension, which keeps no versions.
func lifespanName(words []string) (string, bool) {
	if len(words) == 0 || words[0] == "dim" {
		return "", false
	}
	for _, w := range words {
		switch singular(w) {
		case "price", "tier", "address", "policy", "plan", "tariff":
			return "its name says " + w, true
		}
	}
	switch last := words[len(words)-1]; last {
	case "history", "histories", "version", "versions", "scd":
		return "its name says " + last, true
	}
	return "", false
}

// lifespanKey reports a composite primary key with a start column (*_from or
// effective_*): one row per thing per version.
func lifespanKey(e *Entity) (string, bool) {
	if len(e.PrimaryKey) < 2 {
		return "", false
	}
	for _, k := range e.PrimaryKey {
		if strings.HasSuffix(k, "_from") || strings.HasPrefix(k, "effective_") {
			return "its key includes " + k, true
		}
	}
	return "", false
}

// lifespanGaps lists what §9.5-A requires of p that e lacks.
func lifespanGaps(e *Entity, p period) []string {
	switch {
	case p.span != nil:
		return nil
	case p.from == nil && p.to == nil:
		return []string{"it has no valid_from and valid_to"}
	case p.from == nil:
		return []string{fmt.Sprintf("it has %s but no valid_from", p.to.Name)}
	case p.to == nil:
		return []string{fmt.Sprintf("it has %s but no valid_to", p.from.Name)}
	default:
		return pairGaps(e, p.from, p.to)
	}
}

// pairGaps lists what a valid_from/valid_to pair lacks: one type, a NOT
// NULL start, and in SQL a CHECK reading both, such as valid_to > valid_from
// or valid_to IS NULL OR valid_to > valid_from.
func pairGaps(e *Entity, from, to *Field) []string {
	var gaps []string
	if from.Type != to.Type {
		gaps = append(gaps, fmt.Sprintf("%s is a %s but %s is a %s", from.Name, from.Type,
			to.Name, to.Type))
	}
	if from.Nullable {
		gaps = append(gaps, from.Name+" may be NULL")
	}
	if e.Source == SourceSQL && !slices.ContainsFunc(e.Checks, func(ch Check) bool {
		return containsFold(ch.Columns, from.Name) && containsFold(ch.Columns, to.Name)
	}) {
		gaps = append(gaps, fmt.Sprintf("it has no CHECK (%s > %s)", to.Name, from.Name))
	}
	return gaps
}

func lifespanIssue(e *Entity, why string, gaps []string) issue {
	check := ""
	if e.Source == SourceSQL {
		check = " and CHECK (valid_to > valid_from)"
	}
	return issue{
		entity: e,
		measured: fmt.Sprintf("%s looks like a fact with a lifespan (%s), but %s", e.Name,
			why, strings.Join(gaps, "; ")),
		suggestion: fmt.Sprintf("Give each version valid_from and valid_to of one temporal "+
			"type, valid_from NOT NULL%s, or one tstzrange column with '[)' bounds, so "+
			"exactly one version holds at any instant (§9.5). If %s has no lifespan, "+
			"declare %s.", check, e.Name, annotationHint(e, "lifespan", "false")),
	}
}

// openEndIssues reports the facts that mark an open end the less common way
// when the model uses both NULL and a far-future sentinel (9999-12-31 or
// infinity). A tie reports the sentinels, since NULL with null=open is the
// form §9.3 declares.
func openEndIssues(facts []*Entity) []issue {
	byConvention := map[string][]*Entity{}
	for _, e := range facts {
		if conv := openEnd(e); conv != "" {
			byConvention[conv] = append(byConvention[conv], e)
		}
	}
	nulls, sentinels := byConvention["NULL"], byConvention["a sentinel date"]
	if len(nulls) == 0 || len(sentinels) == 0 {
		return nil
	}
	minority, majority := sentinels, nulls
	if len(nulls) < len(sentinels) {
		minority, majority = nulls, sentinels
	}
	out := make([]issue, 0, len(minority))
	for _, e := range minority {
		to := periodsOf(e, validAxis())[0].to
		out = append(out, issue{
			entity: e, field: to.Name, pos: to.Pos,
			measured: fmt.Sprintf("%s.%s marks an open end with %s, but %s use %s", e.Name,
				to.Name, openEnd(e), andList(entityNames(majority)), openEnd(majority[0])),
			suggestion: fmt.Sprintf("Mark open ends with %s here too, as the other lifespan "+
				"facts do, so one point-in-time predicate fits them all (§9.5).",
				openEnd(majority[0])),
		})
	}
	return out
}

// openEnd names how e marks a version that has not ended: "NULL", "a
// sentinel date", or "" when its valid_to does neither.
func openEnd(e *Entity) string {
	to := periodsOf(e, validAxis())[0].to
	switch {
	case to == nil:
		return ""
	case strings.Contains(to.Default, "9999") || strings.EqualFold(to.Default, "infinity"):
		return "a sentinel date"
	case to.Nullable:
		return "NULL"
	default:
		return ""
	}
}

func entityNames(es []*Entity) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Name
	}
	return out
}

// check96A reports an entity declared audit=bitemporal that cannot answer
// "what did we believe at time T" (rule_plan.md §9.6-A): it needs both a
// valid-time and a transaction-time period, and a key that includes the
// transaction start, or a correction overwrites the version it corrects.
// Undeclared entities are not asked; whether "what was known when" matters
// is the owner's call.
func check96A(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		if audit, _ := e.Annotations.Get("audit"); audit != "bitemporal" {
			continue
		}
		if gaps := bitemporalGaps(e); len(gaps) > 0 {
			out = append(out, issue{
				entity: e,
				measured: fmt.Sprintf("%s is declared audit=bitemporal but %s", e.Name,
					strings.Join(gaps, "; ")),
				suggestion: "Add valid_from/valid_to and txn_from/txn_to timestamptz, make " +
					"the key (business key, txn_from), and close the current version (SET " +
					"txn_to = now()) instead of updating it (§9.6).",
			})
		}
	}
	return out
}

// bitemporalGaps lists what §9.6-A requires of e that it lacks.
func bitemporalGaps(e *Entity) []string {
	ps := periodsOf(e, validAxis(), txnAxis())
	var missing []string
	for i, a := range []timeAxis{validAxis(), txnAxis()} {
		if ps[i].span == nil && ps[i].from == nil {
			missing = append(missing, a.fromRole)
		}
		if ps[i].span == nil && ps[i].to == nil {
			missing = append(missing, a.toRole)
		}
	}
	var gaps []string
	if len(missing) > 0 {
		gaps = append(gaps, "has no "+andList(missing))
	}
	start := cmp.Or(ps[1].span, ps[1].from)
	if start != nil && !inSomeKey(e, start.Name) {
		gaps = append(gaps, fmt.Sprintf("no key includes %s (it has %s)", start.Name,
			describeKeys(e)))
	}
	return gaps
}
