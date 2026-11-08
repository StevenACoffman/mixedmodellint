package mixedmodellint

import (
	"fmt"
	"slices"
	"strings"
)

// copiedField is a field of host that copies field of source: named
// <source>_<field>, or named the same with the same type.
type copiedField struct {
	host   *Entity
	field  *Field
	source *Entity
	from   string
}

func section3Designs() []design {
	return []design{
		{id: "3.1-A", title: "History-required contract", strength: Must, check: check31A},
		{
			id: "3.2-A", title: "Copied attribute without a stated reason", strength: Should,
			check: check32A,
		},
		{
			id: "3.4-A", title: "Master data enforced at write time", strength: Should,
			check: check34A,
		},
	}
}

func check31A(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		for i := range e.Fields {
			f := &e.Fields[i]
			if !c.stateField(e, f) {
				continue
			}
			if is, ok := c.historyIssue(e, f); ok {
				out = append(out, is)
			}
		}
	}
	return out
}

// stateField reports whether f holds a life-cycle state: named status,
// state, stage or phase, or an enumeration of three or more values that the
// corpus updates in place.
func (c *catalog) stateField(e *Entity, f *Field) bool {
	ws := splitWords(f.Name)
	if len(ws) > 0 {
		switch ws[len(ws)-1] {
		case "status", "state", "stage", "phase":
			return true
		}
	}
	return len(enumValues(e, f)) >= 3 && c.updates(e, f.Name)
}

// updates reports whether an UPDATE in the corpus sets e's column col.
func (c *catalog) updates(e *Entity, col string) bool {
	return slices.ContainsFunc(c.writes(), func(w write) bool {
		return w.entity == e && w.kind == QueryUpdate && strings.EqualFold(w.assignment.Column, col)
	})
}

// historyIssue applies §3.1-A to one state field. History is required when
// declared history=required, or when a view groups by the state, which is
// life-cycle analysis; history=latest_only contradicts such a view.
func (c *catalog) historyIssue(e *Entity, f *Field) (issue, bool) {
	policy, _ := e.Annotations.Get("history")
	analyzed := c.groupedBy(e, f.Name)
	switch {
	case policy == "latest_only" && len(analyzed) > 0:
		return issue{
			entity: e, field: f.Name, pos: f.Pos,
			measured: fmt.Sprintf("%s declares history=latest_only, but %s groups by %s.%s, "+
				"which is life-cycle analysis", e.Name, strings.Join(analyzed, ", "), e.Name, f.Name),
			suggestion: fmt.Sprintf("Keep the history of %s (see below), or stop the "+
				"downstream use (§3.1).", f.Name),
		}, true
	case policy != "required" && len(analyzed) == 0:
		return issue{}, false
	}
	missing, found := c.historyForms(e)
	if found && len(missing) == 0 {
		return issue{}, false
	}
	is := issue{
		entity: e, field: f.Name, pos: f.Pos,
		measured: fmt.Sprintf("%s.%s needs its history, but only the current state is stored",
			e.Name, f.Name),
		suggestion: fmt.Sprintf("Add %s_events(%s_id REFERENCES %s, event_type, actor_id, "+
			"occurred_at TIMESTAMPTZ NOT NULL), or keep temporal records (a %s_versions table "+
			"with valid_from/valid_to), or declare history_in=<event schema>. If only the latest "+
			"state matters, declare history=latest_only (§3.1).", e.Canonical, e.Canonical,
			e.Name, e.Canonical),
	}
	if found {
		is.measured = fmt.Sprintf("%s.%s has an event history, but it lacks %s", e.Name, f.Name,
			strings.Join(missing, ", "))
	}
	return is, true
}

// groupedBy names the views that group by e's column col.
func (c *catalog) groupedBy(e *Entity, col string) []string {
	var out []string
	for i := range c.queries {
		q := &c.queries[i]
		if q.Kind != QueryCreateView {
			continue
		}
		s := c.scopeOf(q)
		for _, g := range q.GroupBy {
			if s.entity(g.Relation) == e && strings.EqualFold(g.Column, col) &&
				!slices.Contains(out, q.Target) {
				out = append(out, q.Target)
			}
		}
	}
	return out
}

// historyForms looks for any of the forms of history §3.1-A accepts: an
// event entity, a versions entity with validity columns, or a declared
// external stream. found is false when there is none; missing lists what the
// best event entity lacks of who, what and when, and is empty when a form is
// complete.
func (c *catalog) historyForms(e *Entity) (missing []string, found bool) {
	if up, ok := e.Annotations.Get("history_in"); ok && c.lookup(up, e.Source) != nil {
		return nil, true
	}
	for _, h := range c.entities {
		gaps, ok := c.historyOf(h, e)
		switch {
		case ok && len(gaps) == 0:
			return nil, true
		case ok && (!found || len(gaps) < len(missing)):
			missing, found = gaps, true
		}
	}
	return missing, found
}

// historyOf reports whether h keeps e's history, and what it lacks: nothing
// for a versions entity, who/what/when gaps for an event entity.
func (c *catalog) historyOf(h, e *Entity) (gaps []string, ok bool) {
	switch {
	case h == e || !slices.Contains(c.parents(h), e):
		return nil, false
	case versioned(h):
		return nil, true
	case c.eventEntityFor(h, e):
		return eventGaps(h), true
	default:
		return nil, false
	}
}

// versioned reports whether h keeps temporal records: validity columns.
func versioned(h *Entity) bool {
	_, from := h.Field("valid_from")
	_, to := h.Field("valid_to")
	_, txnFrom := h.Field("txn_from")
	_, txnTo := h.Field("txn_to")
	return (from && to) || (txnFrom && txnTo)
}

// eventEntityFor reports whether h records e's events: declared
// history_of=e, or named e_events, e_history, e_log, e_transitions or
// e_audit.
func (c *catalog) eventEntityFor(h, e *Entity) bool {
	if of, ok := h.Annotations.Get("history_of"); ok {
		return c.lookup(of, e.Source) == e
	}
	for _, suffix := range []string{"event", "history", "log", "transition", "audit"} {
		if h.Canonical == e.Canonical+"_"+suffix {
			return true
		}
	}
	return false
}

// eventGaps lists what an event entity lacks of who did it, what happened,
// and when.
func eventGaps(h *Entity) []string {
	var gaps []string
	for _, g := range []struct {
		has  func(*Field) bool
		name string
	}{
		{isActorField, "who (an actor_id or _by field)"},
		{isEventKindField, "what (an event type or state)"},
		{isEventTimeField, "when (an event time)"},
	} {
		if !slices.ContainsFunc(h.Fields, func(f Field) bool { return g.has(&f) }) {
			gaps = append(gaps, g.name)
		}
	}
	return gaps
}

// isActorField reports a field naming who acted: actor_id, user_id,
// agent_id or a _by field.
func isActorField(f *Field) bool {
	ws := splitWords(f.Name)
	return slices.ContainsFunc(ws, func(w string) bool {
		return w == "actor" || w == "user" || w == "agent"
	}) || (len(ws) > 0 && ws[len(ws)-1] == "by")
}

// isEventKindField reports a field naming what happened: a type, an action,
// or the state moved from or to.
func isEventKindField(f *Field) bool {
	ws := splitWords(f.Name)
	return slices.ContainsFunc(ws, func(w string) bool {
		return w == "type" || w == "action" || w == "status" || w == "state"
	})
}

// isEventTimeField reports a field holding when the event happened.
func isEventTimeField(f *Field) bool {
	return isTemporal(f.Type) && temporalRole(f) == roleEvent
}

// check32A reports fields of operational entities that copy a field of an
// entity they reference, without the stated reason the rule asks for
// (rule_plan.md §3.2-A). Every unexplained copy of the same source field is
// one finding, listing the copies.
func check32A(c *catalog) []issue {
	groups := map[string][]copiedField{}
	var order []string
	for _, e := range c.entities {
		if !operational(e) {
			continue
		}
		for _, cp := range c.copies(e) {
			if validDenormalization(cp.field) {
				continue
			}
			key := cp.source.ID + "." + strings.ToLower(cp.from)
			if _, seen := groups[key]; !seen {
				order = append(order, key)
			}
			groups[key] = append(groups[key], cp)
		}
	}
	out := make([]issue, 0, len(order))
	for _, key := range order {
		out = append(out, copyIssue(groups[key]))
	}
	return out
}

// operational reports whether e is normalized operational data, where a
// copy needs a reason: the oltp layer, or no layer. Event, analytical, wide
// and feature tables are meant to be denormalized.
func operational(e *Entity) bool {
	layer, _ := e.Annotations.Get("layer")
	return layer == "" || layer == "oltp"
}

// copies returns e's fields that copy a field of an entity e references.
// Keys, references, snapshots taken at the transaction (§6.6) and generic
// bookkeeping names are not copies.
func (c *catalog) copies(e *Entity) []copiedField {
	var out []copiedField
	for _, p := range c.parents(e) {
		for i := range e.Fields {
			f := &e.Fields[i]
			if isKeyOrRef(e, f.Name) || fdExcluded(f) || snapshot(e, f) {
				continue
			}
			if from, ok := copiedFrom(f, p); ok {
				out = append(out, copiedField{host: e, field: f, source: p, from: from})
			}
		}
	}
	return out
}

// copiedFrom reports whether f copies a field of p, and which.
func copiedFrom(f *Field, p *Entity) (string, bool) {
	if pf, ok := p.Field(f.Name); ok && pf.Type == f.Type && f.Type != TypeUnknown {
		return pf.Name, true
	}
	if rest, ok := strings.CutPrefix(strings.ToLower(f.Name), p.Canonical+"_"); ok {
		if pf, ok := p.Field(rest); ok {
			return pf.Name, true
		}
	}
	return "", false
}

// snapshot reports a value legitimately captured when the transaction
// happened (§6.6): declared captured_at_transaction, or a price on a line.
func snapshot(e *Entity, f *Field) bool {
	if f.Annotations.Has("captured_at_transaction") {
		return true
	}
	ws, names := splitWords(f.Name), splitWords(e.Name)
	line := slices.ContainsFunc(names, func(w string) bool {
		return w == "line" || w == "lines" || w == "item" || w == "items"
	})
	return line && slices.ContainsFunc(ws, func(w string) bool {
		return w == "price" || w == "cost"
	})
}

// validDenormalization reports whether f states why it is a copy:
// denormalized_for=access_pattern:, latency_sla: or other:, with at least
// three words of reason.
func validDenormalization(f *Field) bool {
	reason, ok := f.Annotations.Get("denormalized_for")
	if !ok {
		return false
	}
	kind, text, found := strings.Cut(reason, ":")
	switch strings.TrimSpace(kind) {
	case "access_pattern", "latency_sla", "other":
		return found && len(strings.Fields(text)) >= 3
	default:
		return false
	}
}

func copyIssue(cps []copiedField) issue {
	first := cps[0]
	hosts := make([]string, 0, len(cps))
	for _, cp := range cps {
		hosts = append(hosts, cp.host.Name+"."+cp.field.Name)
	}
	return issue{
		entity: first.host, field: first.field.Name, pos: first.field.Pos,
		measured: fmt.Sprintf("%s copies %s.%s without a stated reason", strings.Join(hosts, ", "),
			first.source.Name, first.from),
		suggestion: fmt.Sprintf("Read %s through the join to %s, or state why it is "+
			"denormalized: denormalized_for=latency_sla:<what must answer how fast>, "+
			"access_pattern:<the read that needs it> or other:<reason>, in at least three words "+
			"(§3.2).", first.from, first.source.Name),
	}
}

// master reports whether e is master or reference data, which must reject
// bad records at write time.
func master(e *Entity) bool {
	layer, _ := e.Annotations.Get("layer")
	m, _ := e.Annotations.Get("master")
	return layer == "reference" || m == "true"
}

func check34A(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		switch {
		case master(e) && e.Source == SourceSQL:
			out = append(out, c.enforcementGaps(e)...)
		case !e.Annotations.Has("master") && c.masterCandidate(e):
			out = append(out, issue{
				level: SeverityNote, entity: e,
				measured: fmt.Sprintf("%s is referenced by %d entities and named like master "+
					"data, but is not declared master or reference data", e.Name, c.inbound(e)),
				suggestion: "Declare master=true (or layer=reference) so its write-time checks " +
					"are enforced, or master=false if it is not (§3.4).",
			})
		}
	}
	return out
}

// enforcementGaps lists the fields of a master entity that accept bad
// values: required fields that allow NULL, quantities without a range
// check, units without a constraint, and an unconstrained hierarchy.
func (c *catalog) enforcementGaps(e *Entity) []issue {
	var out []issue
	required := map[string]bool{}
	if r, ok := e.Annotations.Get("required"); ok {
		for _, name := range splitList(r) {
			required[strings.ToLower(name)] = true
		}
	}
	for i := range e.Fields {
		f := &e.Fields[i]
		if why := c.unenforced(e, f, required[strings.ToLower(f.Name)]); why != "" {
			out = append(out, issue{
				entity: e, field: f.Name, pos: f.Pos,
				measured: fmt.Sprintf("master entity %s: %s %s", e.Name, f.Name, why),
				suggestion: fmt.Sprintf("Add NOT NULL and a CHECK on %s so bad records are "+
					"rejected at write time (§3.4).", f.Name),
			})
		}
	}
	return out
}

// unenforced says how field f of master e accepts bad values, or "".
func (c *catalog) unenforced(e *Entity, f *Field, required bool) string {
	name := strings.ToLower(f.Name)
	for _, rule := range []struct {
		applies, enforced bool
		why               string
	}{
		{required, !f.Nullable, "is required but allows NULL"},
		{
			c.rx.quantityField.MatchString(name), rangeChecked(e, f),
			"is a quantity with no range check (CHECK (" + f.Name + " > 0))",
		},
		{
			name == "unit_of_measure" || strings.HasSuffix(name, "_uom"),
			len(enumValues(e, f)) > 0 || isKeyOrRef(e, f.Name),
			"is a unit with no constraint to canonical units",
		},
		{name == "level", len(enumValues(e, f)) > 0, "is a hierarchy level with no enumerated values"},
		{name == "parent_id", isKeyOrRef(e, f.Name), "is a hierarchy parent with no foreign key"},
	} {
		if rule.applies && !rule.enforced {
			return rule.why
		}
	}
	return ""
}

// rangeChecked reports whether a CHECK constrains f with a comparison.
func rangeChecked(e *Entity, f *Field) bool {
	return slices.ContainsFunc(e.Checks, func(ch Check) bool {
		return strings.EqualFold(ch.Field, f.Name) && strings.ContainsAny(ch.Expr, "<>")
	})
}

// masterCandidate reports an undeclared entity that looks like master data
// (M2): referenced by five or more entities, named like an item, product,
// location, unit, currency or supplier, and referencing no operational or
// event data itself.
func (c *catalog) masterCandidate(e *Entity) bool {
	if c.inbound(e) < 5 || !c.rx.masterName.MatchString(e.Name) {
		return false
	}
	for _, p := range c.parents(e) {
		layer, _ := p.Annotations.Get("layer")
		if layer == "oltp" || layer == "event" {
			return false
		}
	}
	return true
}
