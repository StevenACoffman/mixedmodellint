package mixedmodellint

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

func section9Designs() []design {
	return []design{
		{id: "9.1-A", title: "Temporal role classifier", strength: Must, check: check91A},
		{id: "9.2-A", title: "Native temporal types", strength: Must, check: check92A},
		{id: "9.4-A", title: "File-entity shape check", strength: Must, check: check94A},
		{id: "9.4-B", title: "Dedup-key structural check", strength: Must, check: check94B},
		{id: "9.8-A", title: "Declared-precision type check", strength: Should, check: check98A},
		{id: "9.8-B", title: "Truncate-on-write audit", strength: Should, check: check98B},
	}
}

// temporalName reports whether a field name says it holds a date or instant:
// its last word is at, on, date, time, ts, timestamp, dob, birthday or
// expires, or it is valid_from/valid_to. Word matching keeps candidate,
// runtime and lifetime out; duration names are excluded.
func temporalName(c *catalog, name string) bool {
	if c.rx.duration.MatchString(name) {
		return false
	}
	ws := splitWords(name)
	if len(ws) == 0 {
		return false
	}
	switch ws[len(ws)-1] {
	case "at", "on", "date", "time", "ts", "timestamp", "dob", "birthday", "expires":
		return true
	}
	lower := strings.ToLower(name)
	return lower == "valid_from" || lower == "valid_to"
}

func check92A(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		for i := range e.Fields {
			f := &e.Fields[i]
			if is, ok := temporalTypeIssue(c, e, f); ok {
				out = append(out, is)
			}
		}
	}
	return out
}

func temporalTypeIssue(c *catalog, e *Entity, f *Field) (issue, bool) {
	base := issue{entity: e, field: f.Name, pos: f.Pos}
	switch {
	case f.Type == TypeTimestampNaive:
		base.measured = fmt.Sprintf("%s.%s is %s, a timestamp without a time zone, so the "+
			"instant it names depends on the writer's zone", e.Name, f.Name, f.RawType)
		base.suggestion = fmt.Sprintf("Store it as timestamptz: ALTER TABLE %s ALTER COLUMN %s "+
			"TYPE timestamptz USING %s AT TIME ZONE '<zone the values were written in>';",
			e.Name, f.Name, f.Name)
		return base, true
	case !temporalName(c, f.Name):
		return issue{}, false
	case f.Type == TypeString:
		base.measured = fmt.Sprintf("%s.%s is named as a date or time but is %s", e.Name,
			f.Name, rawOr(f, "a string"))
		base.suggestion = stringTemporalFix(e, f)
		return base, true
	case f.Type == TypeInt && !f.Annotations.Has("encoding"):
		base.measured = fmt.Sprintf("%s.%s is named as a date or time but is an integer "+
			"with no declared epoch encoding", e.Name, f.Name)
		base.suggestion = fmt.Sprintf("Store it as a native timestamp type, or declare its "+
			"encoding: %s.", fieldAnnotationHint(e, f, "encoding", "unix_ms"))
		return base, true
	default:
		return issue{}, false
	}
}

func stringTemporalFix(e *Entity, f *Field) string {
	switch e.Source {
	case SourceJSONSchema:
		return `Add "format": "date-time" (RFC 3339, with Z or an offset), or "format": "date" ` +
			"for a calendar date, and enable format assertion in the validator."
	case SourceGo:
		return "Use time.Time (or civil.Date for a calendar date) instead of a string."
	default:
		return fmt.Sprintf("Store it as date or timestamptz: ALTER TABLE %s ALTER COLUMN %s "+
			"TYPE timestamptz USING to_timestamp(%s, '<format>');", e.Name, f.Name, f.Name)
	}
}

func rawOr(f *Field, fallback string) string {
	if f.RawType != "" {
		return f.RawType
	}
	return fallback
}

// fieldAnnotationHint writes a field-level annotation in the syntax of e's
// source.
func fieldAnnotationHint(e *Entity, f *Field, key, value string) string {
	switch e.Source {
	case SourceGo:
		return fmt.Sprintf("`mml:\"%s=%s\"` on %s", key, value, f.Name)
	case SourceJSONSchema:
		return fmt.Sprintf(`"x-mml-%s": %q in property %s`, key, value, f.Name)
	default:
		return fmt.Sprintf("COMMENT ON COLUMN %s.%s IS '@%s: %s'", e.Name, f.Name, key, value)
	}
}

func check98B(c *catalog) []issue {
	var out []issue
	seen := map[string]bool{}
	for _, w := range c.writes() {
		if w.field == nil || !eventTime(w.field) {
			continue
		}
		kept := timeUnitRank(finestKept(w.field))
		for i := range w.assignment.Origins {
			o := &w.assignment.Origins[i]
			if timeUnitRank(o.Truncate) <= kept {
				continue
			}
			at := w.at(o)
			key := fmt.Sprintf("%s|%s|%s", at, w.entity.ID, w.field.Name)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, issue{
				entity: w.entity, field: w.field.Name, pos: at,
				measured: fmt.Sprintf("%s.%s is event time, and this write truncates it to the "+
					"%s before it is stored, so the precision the source had cannot be recovered",
					w.entity.Name, w.field.Name, o.Truncate),
				suggestion: fmt.Sprintf("Store the original value and truncate when reading, "+
					"with date_trunc('%s', %s).", o.Truncate, w.field.Name),
			})
		}
	}
	return out
}

// timeScoped reports whether §9.1-A applies to e: a landed, core or
// analytical entity, or one named like events or facts. OLTP tables are
// exempt, because there the write is the business event.
func timeScoped(c *catalog, e *Entity) bool {
	layer, _ := e.Annotations.Get("layer")
	switch layer {
	case "oltp":
		return false
	case "event", "staging", "core", "analytical":
		return true
	default:
		return c.rx.eventName.MatchString(e.Name)
	}
}

func check91A(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		if timeScoped(c, e) {
			out = append(out, timeRoleIssues(e)...)
		}
	}
	return out
}

// timeRoleIssues applies §9.1-A to one entity: it must record when things
// happened, not only when systems saw them, and landed data must record when
// it arrived too.
func timeRoleIssues(e *Entity) []issue {
	roles, temporal := temporalRoles(e)
	out := roleConflicts(e, roles[roleConflict])
	system := slices.Concat(roles[roleIngested], roles[roleProcessed], roles[roleAmbiguous])
	onlySystem := len(roles[roleIngested]) > 0 || len(roles[roleProcessed]) > 0 ||
		(temporal == 1 && len(roles[roleAmbiguous]) == 1)
	switch {
	case len(roles[roleEvent]) == 0 && onlySystem:
		out = append(out, issue{
			entity: e,
			measured: fmt.Sprintf("%s records time only as %s, which is when a system saw "+
				"each row, not when it happened", e.Name, strings.Join(system, ", ")),
			suggestion: fmt.Sprintf("Add an event-time column (e.g. %s_occurred_at timestamptz "+
				"NOT NULL) populated from the source, keep %s as the ingestion or processing "+
				"time, and declare time=event|ingested|processed on each (§9.1).", singular(lowerName(e)),
				strings.Join(system, ", ")),
		})
	case landed(e) && len(roles[roleIngested]) == 0 && temporal > 0:
		out = append(out, issue{
			level:  SeverityWarning,
			entity: e,
			measured: fmt.Sprintf("%s holds landed data but has no ingestion-time column, so "+
				"late arrivals cannot be told from late events", e.Name),
			suggestion: "Add ingested_at timestamptz NOT NULL DEFAULT now(), and declare " +
				"time=ingested on it (§9.1).",
		})
	}
	return out
}

// temporalRoles groups e's date and timestamp fields by role, and counts
// them.
func temporalRoles(e *Entity) (roles map[timeRole][]string, n int) {
	roles = map[timeRole][]string{}
	for i := range e.Fields {
		f := &e.Fields[i]
		if isTemporal(f.Type) {
			n++
			roles[temporalRole(f)] = append(roles[temporalRole(f)], f.Name)
		}
	}
	return roles, n
}

func roleConflicts(e *Entity, names []string) []issue {
	out := make([]issue, 0, len(names))
	for _, name := range names {
		f, _ := e.Field(name)
		out = append(out, issue{
			entity: e, field: name, pos: f.Pos,
			measured: fmt.Sprintf("%s.%s is named as two kinds of time at once", e.Name, name),
			suggestion: fmt.Sprintf("Rename %s, or declare time=event|ingested|processed on it "+
				"(§9.1).", name),
		})
	}
	return out
}

// landed reports whether e holds data as it arrived from elsewhere.
func landed(e *Entity) bool {
	layer, _ := e.Annotations.Get("layer")
	return layer == "event" || layer == "staging"
}

func lowerName(e *Entity) string { return strings.ToLower(e.Canonical) }

// fileEntity reports whether e describes stored files (rule_plan.md §9.4-A):
// declared layer=unstructured or role=file, or a location field together
// with a file-descriptor field.
func fileEntity(c *catalog, e *Entity) bool {
	layer, _ := e.Annotations.Get("layer")
	role, _ := e.Annotations.Get("role")
	if layer == "unstructured" || role == "file" {
		return true
	}
	return slices.ContainsFunc(
		e.Fields,
		func(f Field) bool { return c.rx.fileLocation.MatchString(f.Name) },
	) &&
		slices.ContainsFunc(
			e.Fields,
			func(f Field) bool { return c.rx.fileDescriptor.MatchString(f.Name) },
		)
}

func check94A(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		if !fileEntity(c, e) {
			continue
		}
		if missing := fileShapeGaps(c, e); len(missing) > 0 {
			out = append(out, issue{
				entity: e,
				measured: fmt.Sprintf("%s is a file entity but lacks %s", e.Name,
					strings.Join(missing, "; ")),
				suggestion: "Add captured_at timestamptz (from EXIF DateTimeOriginal with " +
					"OffsetTimeOriginal, or document metadata) and ingested_at timestamptz NOT " +
					"NULL DEFAULT now(). To deduplicate, add content_sha256 char(64) NOT NULL, or " +
					"declare dedup=captured_at,ingested_at to compare by those times (§9.4).",
			})
		}
	}
	return out
}

// fileShapeGaps lists what a file entity is missing of §9.4-A's shape: a
// creation time, a separate ingestion time, and a basis for deduplication.
func fileShapeGaps(c *catalog, e *Entity) []string {
	created, ingested := fileTimes(c, e)
	var missing []string
	if created == nil {
		missing = append(missing, "a creation-time field (when the file was made)")
	}
	if ingested == nil {
		missing = append(missing, "an ingestion-time field (when it arrived)")
	}
	hasHash := slices.ContainsFunc(
		e.Fields,
		func(f Field) bool { return c.rx.contentHash.MatchString(f.Name) },
	)
	if !hasHash && !dedupOnTimes(e, created, ingested) {
		missing = append(missing, "a content hash or a dedup= declaration over the creation "+
			"and ingestion times")
	}
	return missing
}

// fileTimes returns e's creation-time and ingestion-time fields, or nil.
func fileTimes(c *catalog, e *Entity) (created, ingested *Field) {
	for i := range e.Fields {
		f := &e.Fields[i]
		if !isTemporal(f.Type) {
			continue
		}
		t, _ := f.Annotations.Get("time")
		if created == nil && fileTime(t, "event", c.rx.fileCreated, f.Name) {
			created = f
		}
		if ingested == nil && fileTime(t, "ingested", c.rx.fileIngested, f.Name) {
			ingested = f
		}
	}
	return created, ingested
}

// fileTime reports whether a field is the file time named role: declared as
// it, or, with no time= declaration, named like it.
func fileTime(declared, role string, rx *regexp.Regexp, name string) bool {
	if declared != "" {
		return declared == role
	}
	return rx.MatchString(name)
}

// dedupOnTimes reports whether dedup= names both the creation and the
// ingestion field.
func dedupOnTimes(e *Entity, created, ingested *Field) bool {
	d, ok := e.Annotations.Get("dedup")
	if !ok || created == nil || ingested == nil {
		return false
	}
	cols := splitList(d)
	return containsFold(cols, created.Name) && containsFold(cols, ingested.Name)
}

func check94B(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		if !fileEntity(c, e) {
			continue
		}
		if is, ok := nameDedup(c, e); ok {
			out = append(out, is)
		}
	}
	return out
}

// nameDedup reports a file entity deduplicated by file name: dedup= naming
// a file-name column, or a key made only of file-name columns. An entity
// with no key at all gets a note, since dedup may live in ingestion code.
func nameDedup(c *catalog, e *Entity) (issue, bool) {
	names := func(cols []string) bool {
		return len(cols) > 0 && !slices.ContainsFunc(cols, func(col string) bool {
			return !c.rx.fileName.MatchString(col)
		})
	}
	if d, ok := e.Annotations.Get("dedup"); ok {
		if slices.ContainsFunc(
			splitList(d),
			func(col string) bool { return c.rx.fileName.MatchString(col) },
		) {
			return nameDedupIssue(e, "dedup= names a file-name column"), true
		}
		return issue{}, false
	}
	for _, k := range e.Keys() {
		if names(k) {
			return nameDedupIssue(e, "the only uniqueness is on "+strings.Join(k, ", ")), true
		}
	}
	if len(e.Keys()) == 0 {
		return issue{
			level: SeverityNote, entity: e,
			measured: fmt.Sprintf("%s enforces no uniqueness, so duplicates are caught only "+
				"if ingestion code checks", e.Name),
			suggestion: "Add UNIQUE (content_sha256), or UNIQUE (<scope>, content_sha256), so " +
				"the database rejects duplicate files (§9.4).",
		}, true
	}
	return issue{}, false
}

func nameDedupIssue(e *Entity, why string) issue {
	return issue{
		entity: e,
		measured: fmt.Sprintf("%s deduplicates files by name (%s), so copies with different "+
			"names duplicate and different files with the same name collide", e.Name, why),
		suggestion: "Deduplicate by content, with UNIQUE (content_sha256) or UNIQUE (<scope>, " +
			"content_sha256), or by creation and ingestion times with dedup=. Keep the object " +
			"key unique as a storage address if needed, but never use the file name (§9.4).",
	}
}

func check98A(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		for i := range e.Fields {
			f := &e.Fields[i]
			if is, ok := c.precisionIssue(e, f); ok {
				out = append(out, is)
			}
		}
	}
	return out
}

// precisionIssue applies §9.8-A to one event-time field: a DATE (or JSON
// format: date) cannot hold an instant named _at, and a timestamp with fewer
// fractional digits than the source provides loses it. The source's
// precision is the field's precision= declaration, or microseconds,
// PostgreSQL's finest.
func (c *catalog) precisionIssue(e *Entity, f *Field) (issue, bool) {
	if !isTemporal(f.Type) || temporalRole(f) != roleEvent {
		return issue{}, false
	}
	is := issue{entity: e, field: f.Name, pos: f.Pos}
	ws := splitWords(f.Name)
	if f.Type == TypeDate {
		if len(ws) == 0 || ws[len(ws)-1] != "at" {
			return issue{}, false // a business date (order_date) is a date on purpose
		}
		is.measured = fmt.Sprintf("%s.%s is named as an instant but is %s, which keeps only "+
			"the day", e.Name, f.Name, rawOr(f, "a date"))
		is.suggestion = "Store it as timestamptz (default precision 6), or rename it to a " +
			"_date if only the day matters (§9.8)."
		return is, true
	}
	m := c.rx.timestampPrecision.FindStringSubmatch(f.RawType)
	if m == nil {
		return issue{}, false // no explicit precision: PostgreSQL keeps microseconds
	}
	digits, _ := strconv.Atoi(m[1])
	want := precisionDigits(f)
	if digits >= want {
		return issue{}, false
	}
	is.measured = fmt.Sprintf("%s.%s is %s, which keeps %d fractional digits, but the source "+
		"provides %d", e.Name, f.Name, f.RawType, digits, want)
	is.suggestion = "Use timestamptz with its default precision of 6, and truncate in queries " +
		"instead (§9.8)."
	return is, true
}

// precisionDigits is how many fractional-second digits the source of f
// provides: from precision= (s, ms, us, ns), or six (microseconds) by
// default.
func precisionDigits(f *Field) int {
	p, _ := f.Annotations.Get("precision")
	switch strings.ToLower(p) {
	case "s":
		return 0
	case "ms":
		return 3
	case "ns":
		return 9
	default:
		return 6
	}
}
