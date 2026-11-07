package mixedmodellint_test

import (
	"testing"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// entityOf builds an entity whose annotations are kv, for the contract
// design tables.
func entityOf(name string, kv []string, fields ...m.Field) m.Entity {
	e := table(name, nil, fields...)
	e.Annotations = ann(kv...)
	return e
}

// fieldWith builds a field of type t whose annotations are kv.
func fieldWith(name string, t m.LogicalType, kv ...string) m.Field {
	return m.Field{Name: name, Type: t, Annotations: ann(kv...)}
}

func TestTemporalRoles(t *testing.T) {
	t.Parallel()
	event := []string{"layer", "event"}
	core := []string{"layer", "core"}
	cases := map[string]struct {
		e    m.Entity
		want []string
	}{
		"event time and ingestion time": {
			e: entityOf("clicks", event, col("clicked_at", m.TypeTimestamptz),
				col("ingested_at", m.TypeTimestamptz)),
		},
		"only system time": {
			e:    entityOf("loads", core, col("loaded_at", m.TypeTimestamptz)),
			want: []string{"9.1-A loads"},
		},
		"only processing time": {
			e: entityOf(
				"runs",
				core,
				col("processed_at", m.TypeTimestamptz),
				col("updated_at", m.TypeTimestamptz),
			),
			want: []string{"9.1-A runs"},
		},
		"created_at alone is ambiguous": {
			e:    entityOf("visits", core, col("created_at", m.TypeTimestamptz)),
			want: []string{"9.1-A visits"},
		},
		"created_at beside event time": {
			e: entityOf("visits", core, col("created_at", m.TypeTimestamptz),
				col("visited_at", m.TypeTimestamptz)),
		},
		"created_at declared event time": {
			e: entityOf(
				"visits",
				core,
				fieldWith("created_at", m.TypeTimestamptz, "time", "event"),
			),
		},
		"business time counts as event time": {
			e: entityOf(
				"visits",
				core,
				fieldWith("loaded_at", m.TypeTimestamptz, "time", "business"),
			),
		},
		"a name pointing two ways": {
			e: entityOf("clicks", core, col("clicked_at", m.TypeTimestamptz),
				col("loaded_event_at", m.TypeTimestamptz)),
			want: []string{"9.1-A clicks loaded_event_at"},
		},
		"landed data without ingestion time": {
			e:    entityOf("clicks", event, col("clicked_at", m.TypeTimestamptz)),
			want: []string{"9.1-A clicks"},
		},
		"core data need not record ingestion": {
			e: entityOf("orders", core, col("placed_at", m.TypeTimestamptz)),
		},
		"oltp is exempt": {
			e: entityOf("clicks", []string{"layer", "oltp"}, col("loaded_at", m.TypeTimestamptz)),
		},
		"unscoped entity": {e: entityOf("orders", nil, col("loaded_at", m.TypeTimestamptz))},
		"scoped by an event name": {
			e:    entityOf("page_clicks", nil, col("loaded_at", m.TypeTimestamptz)),
			want: []string{"9.1-A page_clicks"},
		},
		"a business date is event time": {
			e: entityOf(
				"fct_sales",
				nil,
				col("sale_date", m.TypeDate),
				col("loaded_at", m.TypeTimestamptz),
			),
		},
		"a time of day is not a timestamp": {
			e: entityOf(
				"fct_sales",
				nil,
				col("open_time", m.TypeTime),
				col("loaded_at", m.TypeTimestamptz),
			),
			want: []string{"9.1-A fct_sales"},
		},
		"no temporal fields": {e: entityOf("fct_sales", nil, col("amount", m.TypeDecimal))},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := &m.Input{Sources: []m.Source{{Entities: []m.Entity{tc.e}}}}
			equals(t, tc.want, findingsOf(in, "9.1-A"))
		})
	}
}

func TestLandedWithoutIngestionIsAWarning(t *testing.T) {
	t.Parallel()
	r := m.Analyze(&m.Input{Sources: []m.Source{{Entities: []m.Entity{
		entityOf("clicks", []string{"layer", "event"}, col("clicked_at", m.TypeTimestamptz)),
	}}}})
	for i := range r.Findings {
		if r.Findings[i].Design == "9.1-A" {
			equals(t, m.SeverityWarning, r.Findings[i].Severity)
			return
		}
	}
	t.Fatal("no 9.1-A finding")
}

func TestFileEntities(t *testing.T) {
	t.Parallel()
	hash := m.Field{Name: "content_sha256", Type: m.TypeString}
	uri := col("object_uri", m.TypeString)
	fname := col("filename", m.TypeString)
	captured := col("captured_at", m.TypeTimestamptz)
	ingested := col("ingested_at", m.TypeTimestamptz)
	photo := func(kv []string, pk []string, fields ...m.Field) m.Entity {
		e := entityOf("photos", kv, fields...)
		e.PrimaryKey = pk
		return e
	}
	cases := map[string]struct {
		e    m.Entity
		want []string
	}{
		"complete file entity": {
			e: photo(nil, []string{"content_sha256"}, uri, fname, captured, ingested, hash),
		},
		"no creation time": {
			e:    photo(nil, []string{"content_sha256"}, uri, fname, ingested, hash),
			want: []string{"9.4-A photos"},
		},
		"no ingestion time": {
			e:    photo(nil, []string{"content_sha256"}, uri, fname, captured, hash),
			want: []string{"9.4-A photos"},
		},
		"one field declared as both": {
			e: photo(nil, []string{"content_sha256"}, uri, fname, hash,
				fieldWith("seen_at", m.TypeTimestamptz, "time", "event")),
			want: []string{"9.4-A photos"},
		},
		"no hash, dedup on times": {
			e: photo([]string{"dedup", "captured_at, ingested_at"}, []string{"captured_at"},
				uri, fname, captured, ingested),
		},
		"no hash, no dedup": {
			e:    photo(nil, []string{"object_uri"}, uri, fname, captured, ingested),
			want: []string{"9.4-A photos"},
		},
		"declared file entity": {
			e:    photo([]string{"role", "file"}, []string{"id"}, col("id", m.TypeInt)),
			want: []string{"9.4-A photos"},
		},
		"unstructured layer": {
			e:    photo([]string{"layer", "unstructured"}, []string{"id"}, col("id", m.TypeInt)),
			want: []string{"9.4-A photos"},
		},
		"location without descriptor is not a file entity": {
			e: photo(nil, nil, uri),
		},
		"deduplicated by file name": {
			e:    photo(nil, []string{"filename"}, uri, fname, captured, ingested, hash),
			want: []string{"9.4-B photos"},
		},
		"dedup declared on file name": {
			e: photo(
				[]string{"dedup", "filename"},
				[]string{"object_uri"},
				uri,
				fname,
				captured,
				ingested,
				hash,
			),
			want: []string{"9.4-B photos"},
		},
		"no uniqueness at all": {
			e:    photo(nil, nil, uri, fname, captured, ingested, hash),
			want: []string{"9.4-B photos"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := &m.Input{Sources: []m.Source{{Entities: []m.Entity{tc.e}}}}
			equals(t, tc.want, findingsOf(in, "9.4-A", "9.4-B"))
		})
	}
}

func TestLocalTimeNeedsAZone(t *testing.T) {
	t.Parallel()
	local := col("local_start_at", m.TypeTimestampNaive)
	zone := col("start_tz", m.TypeString)
	zoneRef := m.Ref{Fields: []string{"start_tz"}, Target: "time_zones"}
	cases := map[string]struct {
		fields []m.Field
		refs   []m.Ref
		checks []m.Check
		want   []string
	}{
		"a zone constrained by a reference": {
			fields: []m.Field{local, zone}, refs: []m.Ref{zoneRef},
		},
		"a zone constrained by a CHECK of IANA names": {
			fields: []m.Field{local, zone},
			checks: []m.Check{{Field: "start_tz", Enum: []string{"America/Denver", "UTC"}}},
		},
		"a zone with a pattern": {
			fields: []m.Field{local, fieldWith("start_tz", m.TypeString, "pattern", "^UTC$")},
		},
		"no zone field": {
			fields: []m.Field{local}, want: []string{"9.2-B events local_start_at"},
		},
		"a zone field that is not a string": {
			fields: []m.Field{local, col("start_tz", m.TypeInt)},
			want:   []string{"9.2-B events local_start_at"},
		},
		"an unconstrained zone": {
			fields: []m.Field{local, zone}, want: []string{"9.2-B events start_tz"},
		},
		"an abbreviation in the enum": {
			fields: []m.Field{local, {
				Name: "start_tz", Type: m.TypeString,
				Enum: []string{"America/Denver", "MST"},
			}},
			want: []string{"9.2-B events start_tz"},
		},
		"an offset as the default": {
			fields: []m.Field{local, {
				Name: "start_tz", Type: m.TypeString,
				Default: "'UTC-7'::text",
			}},
			refs: []m.Ref{zoneRef}, want: []string{"9.2-B events start_tz"},
		},
		"declared local time": {
			fields: []m.Field{fieldWith("starts_at", m.TypeTimestampNaive, "tz", "local")},
			want:   []string{"9.2-B events starts_at"},
		},
		"declared UTC despite the name": {
			fields: []m.Field{fieldWith("local_start_at", m.TypeTimestamptz, "tz", "utc")},
		},
		"local but not a time": {fields: []m.Field{col("local_currency", m.TypeString)}},
		"a local time stored as a string is still local": {
			fields: []m.Field{col("local_start_at", m.TypeString)},
			want:   []string{"9.2-B events local_start_at"},
		},
		"the zone is the field named like one, not any string": {
			fields: []m.Field{local, col("label", m.TypeString), zone}, refs: []m.Ref{zoneRef},
		},
		"an abbreviation in a CHECK": {
			fields: []m.Field{local, zone},
			checks: []m.Check{{Field: "start_tz", Enum: []string{"America/Denver", "EST"}}},
			want:   []string{"9.2-B events start_tz"},
		},
		"a CHECK on another field does not constrain the zone": {
			fields: []m.Field{local, zone, col("kind", m.TypeString)},
			checks: []m.Check{{Field: "kind", Enum: []string{"a"}}},
			want:   []string{"9.2-B events start_tz"},
		},
		"an IANA default with a cast": {
			fields: []m.Field{local, {
				Name: "start_tz", Type: m.TypeString,
				Default: "'America/Denver'::text",
			}},
			refs: []m.Ref{zoneRef},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e := table("events", nil, tc.fields...)
			e.Refs, e.Checks = tc.refs, tc.checks
			equals(t, tc.want, findingsOf(&m.Input{Sources: []m.Source{{
				Entities: []m.Entity{e},
			}}}, "9.2-B"))
		})
	}
}

func TestIntervalContract(t *testing.T) {
	t.Parallel()
	declared := func(name string, kv ...string) m.Field {
		return fieldWith(name, m.TypeTimestamptz, kv...)
	}
	openEnd := func(name string) m.Field {
		f := declared(name, "end", "exclusive", "null", "open")
		f.Nullable = true
		return f
	}
	cases := map[string]struct {
		kv     []string
		fields []m.Field
		want   []string
	}{
		"a declared open, exclusive end": {
			fields: []m.Field{col("valid_from", m.TypeTimestamptz), openEnd("valid_to")},
		},
		"no end bound declared": {
			fields: []m.Field{
				col("trip_start", m.TypeTimestamptz),
				col("trip_end", m.TypeTimestamptz),
			},
			want: []string{"9.3-B stays trip_end"},
		},
		"a nullable end without null=open": {
			fields: []m.Field{
				col("started_at", m.TypeTimestamptz),
				{
					Name: "ended_at", Type: m.TypeTimestamptz, Nullable: true,
					Annotations: ann("end", "exclusive"),
				},
			},
			want: []string{"9.3-B stays ended_at"},
		},
		"not applicable also explains a NULL end": {
			fields: []m.Field{
				col("started_at", m.TypeTimestamptz),
				{
					Name: "ended_at", Type: m.TypeTimestamptz, Nullable: true,
					Annotations: ann("end", "inclusive", "null", "not_applicable"),
				},
			},
		},
		"ends of different types": {
			fields: []m.Field{
				col("valid_from", m.TypeDate),
				declared("valid_to", "end", "inclusive"),
			},
			want: []string{"9.3-B stays valid_to"},
		},
		"the life-cycle pair": {
			fields: []m.Field{
				col("created_at", m.TypeTimestamptz),
				col("resolved_at", m.TypeTimestamptz),
			},
			want: []string{"9.3-B stays resolved_at"},
		},
		"opened_at is preferred to created_at": {
			fields: []m.Field{
				col("created_at", m.TypeDate),
				col("opened_at", m.TypeTimestamptz),
				declared("closed_at", "end", "exclusive"),
			},
		},
		"a declared interval replaces stem pairing": {
			kv: []string{"interval", "check_in, check_out"},
			fields: []m.Field{
				col("check_in", m.TypeDate),
				col("check_out", m.TypeDate),
				col("trip_start", m.TypeTimestamptz),
				col("trip_end", m.TypeTimestamptz),
			},
			want: []string{"9.3-B stays check_out"},
		},
		"a declared interval naming a missing field": {
			kv:     []string{"interval", "check_in, check_out"},
			fields: []m.Field{col("check_in", m.TypeDate)},
		},
		"a declared interval with one field": {
			kv:     []string{"interval", "check_in"},
			fields: []m.Field{col("check_in", m.TypeDate)},
		},
		"a start with no end":  {fields: []m.Field{col("trip_start", m.TypeTimestamptz)}},
		"an end with no start": {fields: []m.Field{col("resolved_at", m.TypeTimestamptz)}},
		"a declared interval whose ends differ in type": {
			kv: []string{"interval", "check_in, check_out"},
			fields: []m.Field{
				col("check_in", m.TypeDate),
				fieldWith("check_out", m.TypeTimestamptz, "end", "inclusive"),
			},
			want: []string{"9.3-B stays check_out"},
		},
		"other fields before the pair": {
			fields: []m.Field{
				col("stay_id", m.TypeInt), col("trip_start", m.TypeTimestamptz),
				col("trip_end", m.TypeTimestamptz),
			},
			want: []string{"9.3-B stays trip_end"},
		},
		"an end that is not a time": {
			fields: []m.Field{col("trip_start", m.TypeTimestamptz), col("trip_end", m.TypeInt)},
		},
		"begin and end": {
			fields: []m.Field{col("begin_date", m.TypeDate), col("end_date", m.TypeDate)},
			want:   []string{"9.3-B stays end_date"},
		},
		"begins and ends": {
			fields: []m.Field{
				col("begins_at", m.TypeTimestamptz),
				col("ends_at", m.TypeTimestamptz),
			},
			want: []string{"9.3-B stays ends_at"},
		},
		"starts and ends": {
			fields: []m.Field{col("starts_on", m.TypeDate), col("ends_on", m.TypeDate)},
			want:   []string{"9.3-B stays ends_on"},
		},
		"a life-cycle start that is not a time": {
			fields: []m.Field{
				col("created_at", m.TypeString),
				col("resolved_at", m.TypeTimestamptz),
			},
		},
		"names that pair but are not times": {
			fields: []m.Field{col("range_start", m.TypeInt), col("range_end", m.TypeInt)},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e := entityOf("stays", tc.kv, tc.fields...)
			equals(t, tc.want, findingsOf(&m.Input{Sources: []m.Source{{
				Entities: []m.Entity{e},
			}}}, "9.3-B"))
		})
	}
}
