package mixedmodellint

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
)

// Thresholds of §10. rule_plan.md makes the second config.semi.min_typed_cols.
const (
	// minAspects is how many per-aspect scores make a blended one harmless
	// (§10.4-A).
	minAspects = 2
	// minTypedColumns is how many typed columns beside a blob-named JSON
	// column show that the stable fields were shredded (§10.2-A).
	minTypedColumns = 3
)

// census counts e's columns for §10.2-A: blob is its JSON column (a
// payload-named one if there are several), typed counts the other columns
// that are neither keys nor references, and plain those of them that are
// not times either.
type census struct {
	blob         *Field
	typed, plain int
}

func section10Designs() []design {
	return []design{
		{
			id: "10.1-A", title: "Metadata classification contract", strength: Should,
			check: check101A,
		},
		{id: "10.2-A", title: "Raw-blob ratio check", strength: Should, check: check102A},
		{
			id: "10.2-B", title: "Shape consistency of JSON Schema properties", strength: Should,
			check: check102B,
		},
		{id: "10.3-A", title: "Inline-binary detector", strength: Should, check: check103A},
		{id: "10.4-A", title: "Blended-score name check", strength: Should, check: check104A},
	}
}

// check101A reports metadata attributes (audit and load columns such as
// updated_at, _loaded_at or batch_id) that do not say whether they record the
// business or the system that wrote the row, in a class and in their
// description (§10.1). rule_plan.md also counts every temporal field that is
// not the entity's subject; which time is the subject cannot be decided from
// a name, and §9.1 already asks event times to declare their kind, so only
// names in the metadata lexicon count here. The forms of one logical entity
// are judged together, and each field is reported once.
func check101A(c *catalog) []issue {
	var out []issue
	seen := map[string]bool{}
	for _, e := range c.entities {
		if foreignSchema(e) || e.Kind == KindView {
			continue
		}
		for i := range e.Fields {
			f := &e.Fields[i]
			key := e.Canonical + "." + Canonical(f.Name)
			if seen[key] || !c.rx.metadataName.MatchString(f.Name) {
				continue
			}
			seen[key] = true
			if is, ok := c.unclassifiedMetadata(e, f); ok {
				out = append(out, is)
			}
		}
	}
	return out
}

// unclassifiedMetadata applies §10.1-A to f, a metadata attribute of e,
// across the forms of e: it needs a class (declared, or unambiguous from the
// name) and a description that names it.
func (c *catalog) unclassifiedMetadata(e *Entity, f *Field) (issue, bool) {
	class, described := "", false
	for _, form := range c.byCanonical[e.Canonical] {
		if lf := linkedField(form, f); lf != nil {
			class = cmp.Or(class, c.metadataClass(form, lf))
			described = described || c.rx.classWord.MatchString(lf.Doc)
		}
	}
	is := issue{entity: e, field: f.Name, pos: f.Pos}
	switch {
	case class == "":
		is.measured = fmt.Sprintf("%s.%s is a metadata attribute that does not say whether "+
			"it records the business or the system", e.Name, f.Name)
		is.suggestion = fmt.Sprintf("Rename it %s_updated_at (business: when the %s "+
			"changed) or _loaded_at (system: when the pipeline wrote the row), or annotate "+
			"time=business|system (meta= for a field that is not a time), and describe it "+
			"as such (§10.1).", e.Canonical, e.Canonical)
	case !described:
		is.measured = fmt.Sprintf("%s.%s is %s metadata, but no description says so",
			e.Name, f.Name, class)
		is.suggestion = fmt.Sprintf("Describe it in the catalog as %s metadata, e.g. "+
			"COMMENT ON COLUMN %s.%s IS '<what it records> (%s time)' (§10.1).", class,
			e.Name, f.Name, class)
	default:
		return issue{}, false
	}
	return is, true
}

// metadataClass returns "business" or "system" for metadata attribute f of
// e, from a time= or meta= annotation, or from a name only one reading fits:
// a load or pipeline name is system, and a name prefixed with the entity's
// own (customer_updated_at on customer) is business. Otherwise it is "".
func (c *catalog) metadataClass(e *Entity, f *Field) string {
	for _, key := range []string{"time", "meta"} {
		switch v, _ := f.Annotations.Get(key); v {
		case "business", "event":
			return "business"
		case "system", "ingested", "processed":
			return "system"
		}
	}
	switch {
	case c.rx.systemName.MatchString(f.Name):
		return "system"
	case strings.HasPrefix(Canonical(f.Name), e.Canonical+"_"):
		return "business"
	default:
		return ""
	}
}

// check102A reports JSON columns that hold what should be typed columns, and
// the opposite: a schema that declares a home for variable fields while the
// table has none (§10.2).
func check102A(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		if e.Kind == KindView {
			continue
		}
		if is, ok := c.unshredded(e); ok {
			out = append(out, is)
		}
		if is, ok := c.noRemainderColumn(e); ok {
			out = append(out, is)
		}
	}
	return out
}

// unshredded reports e when a JSON column holds its record: every column
// that is not a key, reference or time is JSON, or a column named like a
// payload sits beside fewer than minTypedColumns typed columns. Landing
// tables (layer=staging) and third-party schemas keep payloads as received.
func (c *catalog) unshredded(e *Entity) (issue, bool) {
	layer, _ := e.Annotations.Get("layer")
	if layer == "staging" || foreignSchema(e) {
		return issue{}, false
	}
	cs := c.columnCensus(e)
	if cs.blob == nil {
		return issue{}, false
	}
	payloadOnly := cs.plain == 0
	thinBeside := c.rx.blobName.MatchString(cs.blob.Name) && cs.typed < minTypedColumns
	if !payloadOnly && !thinBeside {
		return issue{}, false
	}
	return issue{
		entity: e, field: cs.blob.Name, pos: cs.blob.Pos,
		measured: fmt.Sprintf("%s keeps its record in %s, with %d typed columns beside it",
			e.Name, cs.blob.Name, cs.typed),
		suggestion: fmt.Sprintf("Shred the stable fields into typed columns (%s) and keep "+
			"only the variable remainder in %s, as properties (§10.2).", c.stableKeys(e),
			cs.blob.Name),
	}, true
}

func (c *catalog) columnCensus(e *Entity) census {
	var cs census
	for i := range e.Fields {
		f := &e.Fields[i]
		switch {
		case f.Type == TypeJSON:
			if cs.blob == nil || c.rx.blobName.MatchString(f.Name) {
				cs.blob = f
			}
		case isKeyOrRef(e, f.Name):
		case isTemporal(f.Type):
			cs.typed++
		default:
			cs.typed++
			cs.plain++
		}
	}
	return cs
}

// stableKeys names the fields e's record always has: the required properties
// of a linked JSON Schema, or a placeholder when there is none.
func (c *catalog) stableKeys(e *Entity) string {
	for _, form := range c.byCanonical[e.Canonical] {
		if form.Source != SourceJSONSchema {
			continue
		}
		var required []string
		for i := range form.Fields {
			if !form.Fields[i].Nullable {
				required = append(required, form.Fields[i].Name)
			}
		}
		if len(required) > 0 {
			return "e.g. " + strings.Join(required, ", ")
		}
	}
	return "the fields every row has"
}

// noRemainderColumn reports a stored form of e with no JSON column while a
// linked JSON Schema declares an extension point for variable fields: every
// new key would need a migration.
func (c *catalog) noRemainderColumn(e *Entity) (issue, bool) {
	if e.Source == SourceJSONSchema ||
		slices.ContainsFunc(e.Fields, func(f Field) bool { return f.Type == TypeJSON }) {
		return issue{}, false
	}
	for _, form := range c.byCanonical[e.Canonical] {
		ext, ok := form.Annotations.Get("extension")
		if form.Source != SourceJSONSchema || !ok {
			continue
		}
		return issue{
			entity: e,
			measured: fmt.Sprintf("%s has no column for the variable %q fields JSON Schema %s "+
				"declares, so every new key will need a migration", e.Name, ext, form.Name),
			suggestion: fmt.Sprintf("Add %s jsonb (or json.RawMessage in Go) to hold the "+
				"remainder (§10.2).", ext),
		}, true
	}
	return issue{}, false
}

// check102B reports JSON Schema properties that must be present, or that
// carry a core concept, but can take more than one shape: a union of kinds,
// or no type at all. oneOf and anyOf are left alone, since a discriminated
// event legitimately uses them and the branches are not loaded.
func check102B(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		if e.Source != SourceJSONSchema {
			continue
		}
		for i := range e.Fields {
			f := &e.Fields[i]
			shape, loose := looseShape(f)
			if !loose {
				continue
			}
			out = append(out, issue{
				entity: e, field: f.Name, pos: f.Pos,
				measured: fmt.Sprintf("%s in %s can be %s", f.Name, e.Name, shape),
				suggestion: fmt.Sprintf("Give %s one shape and move variable fields under a "+
					"declared extension object (x-mml-extension: attributes) with "+
					"\"additionalProperties\": false at the root (§10.2).", f.Name),
			})
		}
	}
	return out
}

// looseShape says what shapes f, a JSON Schema property, can take when it
// must be present or carries a core concept and can take more than one.
func looseShape(f *Field) (string, bool) {
	if f.Nullable && !f.Annotations.Has("core") {
		return "", false
	}
	switch {
	case strings.Contains(f.RawType, "|"):
		return "any of " + strings.ReplaceAll(f.RawType, "|", ", "), true
	case f.Type == TypeUnknown && f.RawType == "":
		return "anything, since it has no type", true
	default:
		return "", false
	}
}

// check103A reports files kept in rows, and references to stored objects
// that do not say who stored the object, when, what it is or which version
// (§10.3). The reference names are narrower than rule_plan.md's any _url, so
// a customer's website_url is not taken for a stored file.
func check103A(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		if foreignSchema(e) {
			continue
		}
		for i := range e.Fields {
			f := &e.Fields[i]
			if c.inlineContent(f) {
				out = append(out, issue{
					entity: e, field: f.Name, pos: f.Pos,
					measured: fmt.Sprintf("%s.%s stores file content in the row", e.Name, f.Name),
					suggestion: fmt.Sprintf("Store the object in object storage and keep "+
						"%s_uri text NOT NULL, uploaded_at, uploaded_by, content_type and "+
						"content_sha256 here instead (§10.3).", e.Canonical),
				})
			}
			if missing := c.objectMetaGaps(e, f); len(missing) > 0 {
				out = append(out, issue{
					entity: e, field: f.Name, pos: f.Pos,
					measured: fmt.Sprintf("%s.%s references a stored object, but %s does not "+
						"record %s", e.Name, f.Name, e.Name, strings.Join(missing, ", or ")),
					suggestion: fmt.Sprintf("Add %s beside %s, so who stored the object, "+
						"when, what it is and which version are all known (§10.3).",
						c.objectMetaExamples(missing), f.Name),
				})
			}
		}
	}
	return out
}

// inlineContent reports whether f holds a file's content: bytes that are
// not a hash, key or token, or text named for an image, document or other
// file.
func (c *catalog) inlineContent(f *Field) bool {
	name := snakeName(f.Name)
	switch f.Type {
	case TypeBytes:
		return !c.rx.secretBytes.MatchString(name)
	case TypeString:
		return c.rx.mediaText.MatchString(name)
	default:
		return false
	}
}

// objectMetaGaps returns the kinds of object metadata (who, when, what,
// version) e lacks when f references a stored object, or nil.
func (c *catalog) objectMetaGaps(e *Entity, f *Field) []string {
	if !c.rx.objectRef.MatchString(snakeName(f.Name)) {
		return nil
	}
	var missing []string
	for _, k := range c.rx.objectMeta {
		if !slices.ContainsFunc(e.Fields, func(g Field) bool {
			return k.name.MatchString(snakeName(g.Name))
		}) {
			missing = append(missing, k.kind)
		}
	}
	return missing
}

// objectMetaExamples names a column for each missing kind.
func (c *catalog) objectMetaExamples(kinds []string) string {
	var out []string
	for _, k := range c.rx.objectMeta {
		if slices.Contains(kinds, k.kind) {
			out = append(out, k.example)
		}
	}
	return strings.Join(out, ", ")
}

// check104A reports a numeric sentiment score that blends every aspect into
// one, with no per-aspect scores beside it on the entity or in a child table
// keyed by aspect (§10.4). A blend kept alongside the aspects is allowed.
func check104A(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		if c.hasAspectScores(e) {
			continue
		}
		for i := range e.Fields {
			f := &e.Fields[i]
			if !c.rx.blendedSentiment.MatchString(snakeName(f.Name)) || !isNumeric(f.Type) {
				continue
			}
			out = append(out, issue{
				entity: e, field: f.Name, pos: f.Pos,
				measured: fmt.Sprintf("%s.%s blends every aspect into one score, so opposing "+
					"opinions cancel out", e.Name, f.Name),
				suggestion: fmt.Sprintf("Store one score per aspect, as columns such as "+
					"sentiment_product and sentiment_shipping, or as rows of "+
					"%s_aspect_sentiment(%s_id, aspect, score) with aspect constrained to an "+
					"enum (§10.4).", e.Canonical, e.Canonical),
			})
		}
	}
	return out
}

// hasAspectScores reports whether e keeps sentiment per aspect: at least
// minAspects sentiment_<aspect> columns, or a sentiment table with an aspect
// column that references e.
func (c *catalog) hasAspectScores(e *Entity) bool {
	n := 0
	for i := range e.Fields {
		name := snakeName(e.Fields[i].Name)
		if c.rx.aspectSentiment.MatchString(name) && !c.rx.blendedSentiment.MatchString(name) {
			n++
		}
	}
	if n >= minAspects {
		return true
	}
	for _, t := range c.entities {
		if _, ok := t.Field("aspect"); ok && strings.Contains(t.Canonical, "sentiment") &&
			slices.Contains(c.parents(t), e) {
			return true
		}
	}
	return false
}
