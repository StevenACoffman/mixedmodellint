package mixedmodellint

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// interval is a start and end field of one entity that bound one period.
type interval struct {
	start, end *Field
}

func section9Designs() []design {
	return []design{
		{id: "9.1-A", title: "Temporal role classifier", strength: Must, check: check91A},
		{id: "9.1-B", title: "Reporting predicate audit", strength: Must, check: check91B},
		{
			id: "9.1-C", title: "Cross-source time-role consistency", strength: Must,
			check: check91C,
		},
		{id: "9.2-A", title: "Native temporal types", strength: Must, check: check92A},
		{id: "9.2-B", title: "Local time needs a zone", strength: Must, check: check92B},
		{id: "9.3-B", title: "Duration and open-end contract", strength: Must, check: check93B},
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

func check92B(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		for i := range e.Fields {
			f := &e.Fields[i]
			if localTime(c, f) {
				if is, ok := zoneIssue(c, e, f); ok {
					out = append(out, is)
				}
			}
		}
	}
	return out
}

// localTime reports whether f holds a wall-clock time (§9.2-B): declared
// tz=local, or a date or time named local_. The name alone is only trusted
// on a temporal field, so local_currency is not a local time.
func localTime(c *catalog, f *Field) bool {
	if tz, ok := f.Annotations.Get("tz"); ok {
		return tz == "local"
	}
	return c.rx.localName.MatchString(f.Name) && (isTemporal(f.Type) || temporalName(c, f.Name))
}

// zoneIssue checks that e stores the IANA zone that local time f is in: a
// string field named like a zone, constrained to zone names by a reference,
// an enum or a CHECK, with no value that is an offset or an abbreviation.
func zoneIssue(c *catalog, e *Entity, f *Field) (issue, bool) {
	zone := zoneField(c, e)
	if zone == nil {
		stem := strings.TrimSuffix(strings.TrimPrefix(strings.ToLower(f.Name), "local_"), "_at")
		return issue{
			entity: e, field: f.Name, pos: f.Pos,
			measured: fmt.Sprintf("%s.%s is local time but %s has no time-zone field", e.Name,
				f.Name, e.Name),
			suggestion: fmt.Sprintf("Add %[1]s_tz text NOT NULL REFERENCES time_zones(name), "+
				"holding a name such as 'America/Denver'. Keep %[2]s as an absolute "+
				"timestamptz and derive local time with AT TIME ZONE %[1]s_tz (§9.2).",
				stem, f.Name),
		}, true
	}
	is := issue{entity: e, field: zone.Name, pos: zone.Pos, suggestion: fmt.Sprintf(
		"Constrain %s to IANA zone names such as 'America/Denver', with REFERENCES "+
			"time_zones(name) or an enum generated from tzdata; abbreviations like MST and "+
			"POSIX strings like 'UTC-7', whose sign PostgreSQL inverts, are ambiguous (§9.2).",
		zone.Name)}
	values, constrained := zoneValues(e, zone)
	if !constrained {
		is.measured = fmt.Sprintf("%s.%s holds the zone of %s but nothing constrains it to "+
			"IANA zone names", e.Name, zone.Name, f.Name)
		return is, true
	}
	for _, v := range values {
		if !c.rx.ianaZone.MatchString(v) {
			is.measured = fmt.Sprintf("%s.%s admits %q, which is an offset or an "+
				"abbreviation, not an IANA zone name", e.Name, zone.Name, v)
			return is, true
		}
	}
	return issue{}, false
}

// zoneField returns e's string field named like a time zone, or nil.
func zoneField(c *catalog, e *Entity) *Field {
	for i := range e.Fields {
		f := &e.Fields[i]
		if f.Type == TypeString && c.rx.zoneName.MatchString(f.Name) {
			return f
		}
	}
	return nil
}

// zoneValues returns the values e allows in zone, from its enum, a CHECK
// IN-list and its default, and whether anything constrains it at all: a
// reference to another entity, an enum, a CHECK or a declared pattern.
func zoneValues(e *Entity, zone *Field) (values []string, constrained bool) {
	values = slices.Clone(zone.Enum)
	constrained = len(zone.Enum) > 0 || zone.Annotations.Has("pattern") ||
		slices.ContainsFunc(e.Refs, func(r Ref) bool { return containsFold(r.Fields, zone.Name) })
	for _, ck := range e.Checks {
		if strings.EqualFold(ck.Field, zone.Name) {
			constrained = true
			values = append(values, ck.Enum...)
		}
	}
	if d := strings.Trim(strings.SplitN(zone.Default, "::", 2)[0], "' "); d != "" {
		values = append(values, d)
	}
	return values, constrained
}

func check93B(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		for _, p := range intervalPairs(e) {
			if missing := intervalGaps(p); len(missing) > 0 {
				out = append(out, issue{
					entity: e, field: p.end.Name, pos: p.end.Pos,
					measured: fmt.Sprintf("%s has the interval (%s, %s) but %s", e.Name,
						p.start.Name, p.end.Name, strings.Join(missing, "; ")),
					suggestion: fmt.Sprintf("Declare end=exclusive (or inclusive) on %[2]s, "+
						"null=open on it if the interval may still be running, make both "+
						"columns %[3]s, and compute durations as COALESCE(%[2]s, "+
						"CURRENT_TIMESTAMP) - %[1]s on UTC values (§9.3).", p.start.Name,
						p.end.Name, intervalType(p)),
				})
			}
		}
	}
	return out
}

// intervalPairs returns e's intervals (§9.3-B): the pair an interval=start,end
// annotation names, which replaces stem pairing; or else every pair of
// temporal fields whose names differ only in a start word and an end word
// (valid_from and valid_to, trip_start and trip_end, started_at and
// ended_at), and the life-cycle pair opened_at or created_at with closed_at
// or resolved_at.
func intervalPairs(e *Entity) []interval {
	if v, ok := e.Annotations.Get("interval"); ok {
		names := splitList(v)
		if len(names) != 2 {
			return nil
		}
		start, okStart := e.Field(names[0])
		end, okEnd := e.Field(names[1])
		if !okStart || !okEnd {
			return nil
		}
		return []interval{{start: start, end: end}}
	}
	var out []interval
	for i := range e.Fields {
		start := &e.Fields[i]
		if !isTemporal(start.Type) {
			continue
		}
		if end := pairedEnd(e, start); end != nil {
			out = append(out, interval{start: start, end: end})
		}
	}
	// No stem pair ends in closed_at or resolved_at, so the life-cycle pair
	// never repeats one.
	if p, ok := lifecyclePair(e); ok {
		out = append(out, p)
	}
	return out
}

// pairedEnd returns the temporal field whose name is start's with its start
// word replaced by the matching end word, or nil.
func pairedEnd(e *Entity, start *Field) *Field {
	ws := splitWords(start.Name)
	for i, w := range ws {
		endWord, ok := intervalEnds()[w]
		if !ok {
			continue
		}
		name := strings.Join(slices.Concat(ws[:i], []string{endWord}, ws[i+1:]), "_")
		if end, ok := e.Field(name); ok && isTemporal(end.Type) {
			return end
		}
	}
	return nil
}

// intervalEnds maps each start word to the end word it pairs with.
func intervalEnds() map[string]string {
	return map[string]string{
		"start": "end", "started": "ended", "from": "to", "begin": "end", "begins": "ends",
		"starts": "ends",
	}
}

// lifecyclePair returns the pair opened_at (or else created_at) and
// closed_at (or else resolved_at), when e has one of each.
func lifecyclePair(e *Entity) (interval, bool) {
	pick := func(names ...string) *Field {
		for _, n := range names {
			if f, ok := e.Field(n); ok && isTemporal(f.Type) {
				return f
			}
		}
		return nil
	}
	start, end := pick("opened_at", "created_at"), pick("closed_at", "resolved_at")
	if start == nil || end == nil {
		return interval{}, false
	}
	return interval{start: start, end: end}, true
}

// intervalGaps lists what §9.3-B requires of p that is missing.
func intervalGaps(p interval) []string {
	var gaps []string
	if meaning, _ := p.end.Annotations.Get("null"); p.end.Nullable &&
		meaning != "open" && meaning != "not_applicable" {
		gaps = append(gaps, fmt.Sprintf("%s may be NULL without null=open saying the "+
			"interval is still running", p.end.Name))
	}
	if bound, _ := p.end.Annotations.Get("end"); bound != "exclusive" && bound != "inclusive" {
		gaps = append(gaps, fmt.Sprintf("%s does not declare end=exclusive or "+
			"end=inclusive", p.end.Name))
	}
	if p.start.Type != p.end.Type {
		gaps = append(gaps, fmt.Sprintf("%s is a %s but %s is a %s", p.start.Name,
			p.start.Type, p.end.Name, p.end.Type))
	}
	return gaps
}

// intervalType is the type both ends of p should share: the start's, unless
// it is a naive timestamp, which §9.2 forbids.
func intervalType(p interval) LogicalType {
	if p.start.Type == TypeTimestampNaive {
		return TypeTimestamptz
	}
	return p.start.Type
}

// check91C applies §9.1-C: when a Go or JSON Schema member of a logical
// entity carries event time, the SQL member that stores it must too, or the
// event time is dropped on the way in (and is usually replaced by load
// time).
func check91C(c *catalog) []issue {
	var out []issue
	for _, g := range c.logicalGroups() {
		for _, store := range g {
			if store.Source != SourceSQL || eventTimeField(store) != nil {
				continue
			}
			for _, up := range g {
				ef := eventTimeField(up)
				if up.Source == SourceSQL || ef == nil {
					continue
				}
				out = append(out, issue{
					entity: store,
					measured: fmt.Sprintf(
						"%s carries event time %s but %s stores %s",
						memberName(up),
						ef.Name,
						memberName(store),
						storedTimes(store),
					),
					suggestion: fmt.Sprintf("Persist %s as its own column of %s, declared "+
						"time=event, and do not overwrite it with load time (§9.1).", ef.Name,
						store.Name),
				})
				break
			}
		}
	}
	return out
}

// eventTimeField returns e's first date or timestamp that records when
// something happened (§9.1-A's classification), or nil.
func eventTimeField(e *Entity) *Field {
	for i := range e.Fields {
		if f := &e.Fields[i]; isTemporal(f.Type) && temporalRole(f) == roleEvent {
			return f
		}
	}
	return nil
}

// storedTimes lists e's temporal fields in prose, for a message.
func storedTimes(e *Entity) string {
	var names []string
	for i := range e.Fields {
		if isTemporal(e.Fields[i].Type) {
			names = append(names, e.Fields[i].Name)
		}
	}
	if len(names) == 0 {
		return "no time at all"
	}
	return "only " + strings.Join(names, ", ")
}
