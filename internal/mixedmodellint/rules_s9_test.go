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
