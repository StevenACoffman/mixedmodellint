package mixedmodellint

import (
	"fmt"
	"slices"
	"strings"
)

// keyRename is a key or reference column that changed name: from is gone,
// to is new, and they have the same type.
type keyRename struct {
	from, to string
}

func section4Designs() []design {
	return []design{
		{
			id:       "4.1-A",
			title:    "Key presence and key-type stability",
			strength: Must,
			check:    check41A,
		},
		{
			id: "4.2-A", title: "Multi-source entity requires a resolution contract",
			strength: Must, check: check42A,
		},
		{id: "4.6-A", title: "Derived-entity shape check", strength: Must, check: check46A},
		{
			id:       "4.3-A",
			title:    "Key rename without a mapping table",
			strength: Should,
			check:    check43A,
		},
	}
}

// check43A reports identifiers renamed or rewritten in place without an
// identity-map entity to carry consumers across (rule_plan.md §4.3-A): a key
// or reference column renamed since the previous model, or an UPDATE that
// assigns a primary-key column.
func check43A(c *catalog) []issue {
	var out []issue
	before := map[string]*Entity{}
	for _, e := range c.previous {
		before[e.ID] = e
	}
	for _, e := range c.entities {
		old, ok := before[e.ID]
		if !ok {
			continue
		}
		for _, r := range keyRenames(old, e) {
			if !c.hasIdentityMap(e, r.to, r.from) {
				out = append(out, c.renameIssue(e, r))
			}
		}
	}
	return append(out, c.keyRewrites()...)
}

// keyRenames pairs each key or reference column of old that e no longer has
// with a new key or reference column of e of the same type, in order.
//
// Ensures: each column appears in at most one pair; a removed key column with
// no same-typed replacement is not a rename and is left out.
func keyRenames(old, e *Entity) []keyRename {
	var gone, added []string
	for _, k := range keyColumns(old) {
		if _, ok := e.Field(k); !ok {
			gone = append(gone, k)
		}
	}
	for _, k := range keyColumns(e) {
		if _, ok := old.Field(k); !ok {
			added = append(added, k)
		}
	}
	var out []keyRename
	for _, g := range gone {
		gf, _ := old.Field(g)
		i := slices.IndexFunc(added, func(a string) bool {
			af, ok := e.Field(a)
			return ok && gf != nil && af.Type == gf.Type
		})
		if i >= 0 {
			out = append(out, keyRename{from: g, to: added[i]})
			added = slices.Delete(added, i, i+1)
		}
	}
	return out
}

// keyColumns returns e's primary-key and reference columns, each once.
func keyColumns(e *Entity) []string {
	cols := slices.Clone(e.PrimaryKey)
	for _, r := range e.Refs {
		cols = append(cols, r.Fields...)
	}
	return dedupFold(cols)
}

// hasIdentityMap reports whether some other entity maps identifiers for e:
// one annotated role=identity_map with the new column, or one holding the new
// column beside a source_system or, for a rename, the old column.
func (c *catalog) hasIdentityMap(e *Entity, to, from string) bool {
	for _, m := range c.entities {
		if m == e {
			continue
		}
		_, hasTo := m.Field(to)
		if !hasTo {
			continue
		}
		role, _ := m.Annotations.Get("role")
		_, hasSystem := m.Field("source_system")
		_, hasFrom := m.Field(from)
		// A rewrite in place has no old name, and a child's foreign key shares
		// the new one, so only a distinct old column counts.
		hasFrom = hasFrom && !strings.EqualFold(from, to)
		if role == "identity_map" || hasSystem || hasFrom {
			return true
		}
	}
	return false
}

// inbound counts the entities that reference e.
func (c *catalog) inbound(e *Entity) int {
	n := 0
	for _, o := range c.entities {
		if o != e && slices.Contains(c.parents(o), e) {
			n++
		}
	}
	return n
}

func (c *catalog) renameIssue(e *Entity, r keyRename) issue {
	pos := e.Pos
	if f, ok := e.Field(r.to); ok {
		pos = f.Pos
	}
	return issue{
		entity: e, field: r.to, pos: pos,
		measured: fmt.Sprintf("identifier %s.%s was renamed to %s since mml.lock.json, and %s",
			e.Name, r.from, r.to, references(c.inbound(e), e.Name)),
		suggestion: identityMapHint(e, r.to),
	}
}

// keyRewrites reports UPDATEs that assign a primary-key column: an
// identifier rewritten in place breaks every consumer holding the old value.
func (c *catalog) keyRewrites() []issue {
	var out []issue
	for i := range c.queries {
		q := &c.queries[i]
		if q.Kind != QueryUpdate {
			continue
		}
		e := c.lookup(q.Target, SourceSQL)
		if e == nil {
			continue
		}
		for _, a := range q.Assignments {
			if containsFold(e.PrimaryKey, a.Column) && !c.hasIdentityMap(e, a.Column, a.Column) {
				out = append(out, issue{
					entity: e,
					field:  a.Column,
					pos:    q.Pos,
					measured: fmt.Sprintf(
						"an UPDATE rewrites the identifier %s.%s in place, and %s",
						e.Name,
						a.Column,
						references(c.inbound(e), e.Name),
					),
					suggestion: identityMapHint(e, a.Column),
				})
			}
		}
	}
	return out
}

// references says how many entities reference name.
func references(n int, name string) string {
	switch n {
	case 0:
		return "no entity references " + name
	case 1:
		return "1 entity references " + name
	default:
		return fmt.Sprintf("%d entities reference %s", n, name)
	}
}

func identityMapHint(e *Entity, col string) string {
	return fmt.Sprintf("Introduce %s_id_map(%s, source_system, source_id) and move consumers "+
		"to it before changing identifiers in place (§4.3). Then run mixedmodellint lock to "+
		"record the new shape.", strings.ToLower(e.Canonical), col)
}

func check41A(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		layer, _ := e.Annotations.Get("layer")
		if e.Kind == KindView || e.Annotations.Has("value-object") || layer == "external" {
			continue
		}
		if len(e.PrimaryKey) == 0 {
			out = append(out, issue{
				entity:   e,
				measured: fmt.Sprintf("%s has no primary key", e.Name),
				suggestion: fmt.Sprintf("Add a system-generated identifier, for example %s_id "+
					"BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY (Go: an ID field, or one "+
					"tagged mml:\"pk\"; JSON: x-mml-key) (§4.1).", e.Canonical),
			})
			continue
		}
		out = append(out, unstableKeys(c, e)...)
	}
	return out
}

// unstableKeys reports primary-key columns whose values can change (an email,
// a name) or repeat (a start time that is the whole key).
func unstableKeys(c *catalog, e *Entity) []issue {
	var out []issue
	identified := slices.ContainsFunc(e.PrimaryKey, func(k string) bool { return identifier(e, k) })
	for _, k := range e.PrimaryKey {
		f, ok := e.Field(k)
		if !ok {
			continue
		}
		if reason := keyInstability(c, f, identified); reason != "" {
			out = append(out, issue{
				entity: e,
				field:  k,
				pos:    f.Pos,
				measured: fmt.Sprintf(
					"%s.%s is used as the primary key, but %s",
					e.Name,
					k,
					reason,
				),
				suggestion: fmt.Sprintf("Add a surrogate %s_id primary key and keep %s as UNIQUE "+
					"(or a plain indexed attribute if it can repeat) (§4.1).", e.Canonical, k),
			})
		}
	}
	return out
}

// keyInstability says why key column f is unstable, or "": a natural value
// that can change (an email, a name), or a time that is the key without an
// identifier beside it.
func keyInstability(c *catalog, f *Field, identified bool) string {
	switch {
	case c.rx.mutableKey.MatchString(f.Name) || mutableFormat(f):
		return "its value can change"
	case identified:
		return ""
	case c.rx.timeKey.MatchString(f.Name) || isTemporal(f.Type):
		return "two rows can share a time, and a re-run changes it"
	default:
		return ""
	}
}

// mutableFormat reports a JSON Schema string format whose values change: an
// email address or a host name.
func mutableFormat(f *Field) bool {
	for _, format := range []string{"email", "idn-email", "hostname"} {
		if strings.Contains(f.RawType, "(format "+format+")") {
			return true
		}
	}
	return false
}

// identifier reports whether column k of e identifies something by itself:
// an ID or a reference, so a time beside it is part of a composite key.
func identifier(e *Entity, k string) bool {
	if isKeyOrRef(e, k) &&
		slices.ContainsFunc(e.Refs, func(r Ref) bool { return containsFold(r.Fields, k) }) {
		return true
	}
	ws := splitWords(k)
	return len(ws) > 0 && (ws[len(ws)-1] == "id" || ws[len(ws)-1] == "key")
}

func check42A(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		systems, multi := c.sourceSystems(e)
		if !multi || mappingShaped(e) || c.resolutionTarget(e) {
			continue
		}
		if problems := c.resolutionProblems(e, systems); len(problems) > 0 {
			out = append(out, issue{
				entity: e,
				measured: fmt.Sprintf("%s merges records from several systems (%s), but %s",
					e.Name, systemList(systems), strings.Join(problems, "; ")),
				suggestion: fmt.Sprintf(
					"Declare resolution=<the mapping entity from source IDs to "+
						"%s_id> and resolution_owner=<team>, with a mapping of %s_id, source_system "+
						"and source_id (§4.2, §4.3).",
					e.Canonical,
					e.Canonical,
				),
			})
		}
	}
	return out
}

// resolutionTarget reports whether another entity names e as its
// resolution: e is then the mapping, not an entity that needs one.
func (c *catalog) resolutionTarget(e *Entity) bool {
	return slices.ContainsFunc(c.entities, func(o *Entity) bool {
		r, ok := o.Annotations.Get("resolution")
		return ok && o != e && c.lookup(r, o.Source) == e
	})
}

// systemList names the systems an entity's records come from, or says that
// a source_system column names them.
func systemList(systems []string) string {
	if len(systems) == 0 {
		return "named in its source_system column"
	}
	return strings.Join(systems, ", ")
}

// mappingShaped reports whether e maps source-system IDs to an entity's ID,
// which makes it the resolution rather than an entity needing one: declared
// role=identity_map, or a source_system column beside a source ID column.
func mappingShaped(e *Entity) bool {
	if role, _ := e.Annotations.Get("role"); role == "identity_map" {
		return true
	}
	_, system := e.Field("source_system")
	return system && hasSourceID(e)
}

func hasSourceID(e *Entity) bool {
	return slices.ContainsFunc([]string{"source_id", "source_key", "external_id"},
		func(n string) bool { _, ok := e.Field(n); return ok })
}

// sourceSystems returns the systems e's records come from, and whether
// there are several: two or more source-ID fields (crm_id, stripe_id), a
// source_system column, or two or more declared sources.
func (c *catalog) sourceSystems(e *Entity) ([]string, bool) {
	var systems []string
	for i := range e.Fields {
		if m := c.rx.sourceKey.FindStringSubmatch(e.Fields[i].Name); m != nil {
			systems = append(systems, strings.ToLower(m[2]))
		}
	}
	if s, ok := e.Annotations.Get("sources"); ok && len(splitList(s)) >= 2 {
		return splitList(s), true
	}
	_, column := e.Field("source_system")
	return systems, len(systems) >= 2 || column
}

// resolutionProblems lists what e's identity-resolution contract lacks: the
// two declarations, a mapping entity of the right shape whose allowed
// systems cover every source, and an owner the repository knows.
func (c *catalog) resolutionProblems(e *Entity, systems []string) []string {
	var problems []string
	target, hasRes := e.Annotations.Get("resolution")
	owner, hasOwner := e.Annotations.Get("resolution_owner")
	if !hasRes {
		problems = append(problems, "declares no resolution")
	} else {
		problems = append(problems, c.mappingProblems(e, target, systems)...)
	}
	known := c.ownershipRecord(owner) != nil || slices.Contains(c.artifacts.Codeowners, owner)
	registered := len(c.artifacts.Ownership) > 0 || c.artifacts.Codeowners != nil
	switch {
	case !hasOwner:
		problems = append(problems, "declares no resolution_owner")
	case registered && !known:
		problems = append(problems, fmt.Sprintf("resolution_owner %s is neither in an ownership "+
			"record nor in CODEOWNERS", owner))
	case !registered && !c.rx.principal.MatchString(owner):
		problems = append(problems, fmt.Sprintf("resolution_owner %q is not a team, group, role "+
			"or CODEOWNERS principal", owner))
	}
	return problems
}

// mappingProblems checks the resolution target: it must exist and map
// source IDs to e's ID, and its allowed source systems, when constrained,
// must include every system e takes records from.
func (c *catalog) mappingProblems(e *Entity, target string, systems []string) []string {
	m := c.lookup(target, e.Source)
	if m == nil {
		return []string{fmt.Sprintf("resolution %s names no entity", target)}
	}
	var problems, lacking []string
	if _, ok := m.Field(e.Canonical + "_id"); !ok {
		lacking = append(lacking, e.Canonical+"_id")
	}
	sys, hasSystem := m.Field("source_system")
	if !hasSystem {
		lacking = append(lacking, "source_system")
	}
	if !hasSourceID(m) {
		lacking = append(lacking, "source_id")
	}
	if len(lacking) > 0 {
		problems = append(problems, fmt.Sprintf("resolution %s lacks %s", m.Name,
			strings.Join(lacking, ", ")))
	}
	if hasSystem {
		if allowed := enumValues(m, sys); len(allowed) > 0 {
			for _, s := range systems {
				if !containsFold(allowed, s) {
					problems = append(problems, fmt.Sprintf("%s does not allow source system %s",
						m.Name, s))
				}
			}
		}
	}
	return problems
}

func check46A(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		source, signals := c.derivedSignals(e)
		switch {
		case signals == 1:
			out = append(out, issue{
				level: SeverityNote, entity: e,
				measured: fmt.Sprintf("%s looks derived from a source file, but only by its "+
					"name or only by its shape", e.Name),
				suggestion: fmt.Sprintf("Declare derived_from=<source entity> on %s if it is "+
					"cut out of a source, so its key can be checked (§4.6).", e.Name),
			})
		case signals >= 2:
			if gaps := derivedGaps(c, e, source); len(gaps) > 0 {
				out = append(out, issue{
					entity: e,
					measured: fmt.Sprintf(
						"%s is derived from %s but %s",
						e.Name,
						sourceName(source),
						strings.Join(gaps, "; "),
					),
					suggestion: fmt.Sprintf(
						"Identify each instance by (%s_id, <location fields>), "+
							"for example detections(video_id, ts_start, ts_end, bbox, label), with "+
							"UNIQUE (%s_id, <location fields>) (§4.6).",
						sourceName(source),
						sourceName(source),
					),
				})
			}
		}
	}
	return out
}

// derivedSignals returns what e is derived from and how strongly that is
// known: a derived_from declaration counts as two signals; otherwise one for
// a name like detections or chunks, and one for pointing at a file entity or
// holding a file name, path or URL.
func (c *catalog) derivedSignals(e *Entity) (*Entity, int) {
	if src, ok := e.Annotations.Get("derived_from"); ok {
		return c.lookup(src, e.Source), 2
	}
	signals := 0
	if c.rx.derivedName.MatchString(e.Name) {
		signals++
	}
	var source *Entity
	for _, p := range c.parents(e) {
		if fileEntity(c, p) {
			source = p
		}
	}
	// Pointing at a file is the second signal by itself: the rule's failing
	// example, detections(video_filename, label), has no location at all,
	// and that missing location is then the violation.
	if source != nil || slices.ContainsFunc(e.Fields, func(f Field) bool {
		return c.rx.fileRefField.MatchString(f.Name)
	}) {
		signals++
	}
	return source, signals
}

// derivedGaps lists what a derived entity lacks of §4.6-A's shape: a
// reference to its source by ID, location fields, and a key over both.
func derivedGaps(c *catalog, e, source *Entity) []string {
	var gaps []string
	if slices.ContainsFunc(
		e.Fields,
		func(f Field) bool { return c.rx.fileRefField.MatchString(f.Name) },
	) {
		gaps = append(gaps, "refers to its source by file name or path")
	}
	sourceID := sourceIDField(c, e, source)
	located := locationFields(c, e)
	switch {
	case sourceID == "":
		gaps = append(gaps, "has no field referencing the source by ID")
	case len(located) == 0:
		gaps = append(gaps, "has no location fields (time, position, offset or index)")
	case !slices.ContainsFunc(e.Keys(), func(k []string) bool {
		return subsetFold(append([]string{sourceID}, located...), k)
	}):
		gaps = append(gaps, fmt.Sprintf("has no key over %s and its location (%s)", sourceID,
			strings.Join(located, ", ")))
	}
	return gaps
}

// sourceIDField returns the field of e that references source by ID: a
// single-column reference to it, or a field named <source>_id; or "".
func sourceIDField(c *catalog, e, source *Entity) string {
	if source == nil {
		return ""
	}
	if _, ok := e.Field(source.Canonical + "_id"); ok {
		return source.Canonical + "_id"
	}
	for _, r := range e.Refs {
		if len(r.Fields) == 1 && c.lookup(r.Target, e.Source) == source {
			return r.Fields[0]
		}
	}
	return ""
}

// locationFields returns e's fields that say where in its source an instance
// is.
func locationFields(c *catalog, e *Entity) []string {
	var out []string
	for i := range e.Fields {
		if c.rx.locationField.MatchString(e.Fields[i].Name) {
			out = append(out, e.Fields[i].Name)
		}
	}
	return out
}

func sourceName(source *Entity) string {
	if source == nil {
		return "its source"
	}
	return source.Canonical
}
