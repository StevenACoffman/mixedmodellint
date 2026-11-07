package mixedmodellint_test

import (
	"slices"
	"testing"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

func TestKeyStability(t *testing.T) {
	t.Parallel()
	keyed := func(pk []string, fields ...m.Field) m.Entity { return table("runs", pk, fields...) }
	view := keyed(nil)
	view.Kind = m.KindView
	valueObject := keyed(nil)
	valueObject.Annotations = ann("value-object", "true")
	external := keyed(nil)
	external.Annotations = ann("layer", "external")
	runs := keyed(
		[]string{"pipeline_id", "started_at"},
		col("pipeline_id", m.TypeInt),
		col("started_at", m.TypeTimestamptz),
	)
	runs.Refs = []m.Ref{{Fields: []string{"pipeline_id"}, Target: "pipelines"}}
	cases := map[string]struct {
		e    m.Entity
		want []string
	}{
		"surrogate key": {e: keyed([]string{"run_id"}, col("run_id", m.TypeInt))},
		"no key": {
			e:    keyed(nil, col("note", m.TypeString)),
			want: []string{"4.1-A runs"},
		},
		"views need none":         {e: view},
		"value objects need none": {e: valueObject},
		"external data":           {e: external},
		"email as the key": {
			e:    keyed([]string{"email"}, col("email", m.TypeString)),
			want: []string{"4.1-A runs email"},
		},
		"JSON email format": {
			e: keyed(
				[]string{"contact"},
				m.Field{Name: "contact", Type: m.TypeString, RawType: "string (format email)"},
			),
			want: []string{"4.1-A runs contact"},
		},
		"start time as the key": {
			e:    keyed([]string{"started_at"}, col("started_at", m.TypeTimestamptz)),
			want: []string{"4.1-A runs started_at"},
		},
		"a date as the key": {
			e:    keyed([]string{"day"}, col("day", m.TypeDate)),
			want: []string{"4.1-A runs day"},
		},
		"time beside a reference": {e: runs},
		"time beside an id": {
			e: keyed(
				[]string{"series_id", "ts"},
				col("series_id", m.TypeInt),
				col("ts", m.TypeTimestamptz),
			),
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := &m.Input{Sources: []m.Source{{Entities: []m.Entity{tc.e}}}}
			equals(t, tc.want, findingsOf(in, "4.1-A"))
		})
	}
}

func TestIdentityResolution(t *testing.T) {
	t.Parallel()
	customers := func(kv ...string) m.Entity {
		return entityOf("customers", kv, col("customer_id", m.TypeInt), col("crm_id", m.TypeString),
			col("stripe_id", m.TypeString))
	}
	idMap := func(systems ...string) m.Entity {
		return table(
			"customer_map",
			nil,
			col("customer_id", m.TypeInt),
			m.Field{
				Name: "source_system",
				Type: m.TypeString,
				Enum: systems,
			},
			col("source_id", m.TypeString),
		)
	}
	declared := []string{"resolution", "customer_map", "resolution_owner", "team:sales-ops"}
	owners := []m.OwnershipRecord{{Owner: "team:sales-ops"}}
	cases := map[string]struct {
		entities []m.Entity
		owners   []m.OwnershipRecord
		want     []string
	}{
		"resolved": {
			entities: []m.Entity{customers(declared...), idMap("crm", "stripe")},
		},
		"resolved, owner on record": {
			entities: []m.Entity{customers(declared...), idMap()},
			owners:   owners,
		},
		"owner not on record": {
			entities: []m.Entity{
				customers("resolution", "customer_map", "resolution_owner", "team:growth"),
				idMap(),
			},
			owners: owners,
			want:   []string{"4.2-A customers"},
		},
		"owner not a principal": {
			entities: []m.Entity{
				customers("resolution", "customer_map", "resolution_owner", "sales"),
				idMap(),
			},
			want: []string{"4.2-A customers"},
		},
		"undeclared": {
			entities: []m.Entity{customers()},
			want:     []string{"4.2-A customers"},
		},
		"mapping missing": {
			entities: []m.Entity{customers(declared...)},
			want:     []string{"4.2-A customers"},
		},
		"mapping of the wrong shape": {
			entities: []m.Entity{
				customers(declared...),
				table("customer_map", nil, col("customer_id", m.TypeInt)),
			},
			want: []string{"4.2-A customers"},
		},
		"system missing from the map": {
			entities: []m.Entity{customers(declared...), idMap("crm")},
			want:     []string{"4.2-A customers"},
		},
		"one source system": {
			entities: []m.Entity{
				entityOf(
					"customers",
					nil,
					col("customer_id", m.TypeInt),
					col("crm_id", m.TypeString),
				),
			},
		},
		"a source_system column": {
			entities: []m.Entity{
				entityOf(
					"customers",
					nil,
					col("customer_id", m.TypeInt),
					col("source_system", m.TypeString),
				),
			},
			want: []string{"4.2-A customers"},
		},
		"declared sources": {
			entities: []m.Entity{
				entityOf(
					"customers",
					[]string{"sources", "crm, erp"},
					col("customer_id", m.TypeInt),
				),
			},
			want: []string{"4.2-A customers"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := &m.Input{
				Sources:   []m.Source{{Entities: tc.entities}},
				Artifacts: m.Artifacts{Ownership: tc.owners},
			}
			equals(t, tc.want, findingsOf(in, "4.2-A"))
		})
	}
}

func TestDerivedEntities(t *testing.T) {
	t.Parallel()
	videos := entityOf("videos", []string{"role", "file"}, col("video_id", m.TypeInt))
	videos.PrimaryKey = []string{"video_id"}
	detections := func(kv []string, keys [][]string, fields ...m.Field) m.Entity {
		e := entityOf("detections", kv, fields...)
		e.PrimaryKey = []string{"detection_id"}
		e.Uniques = keys
		return e
	}
	refTo := func(e m.Entity) m.Entity {
		e.Refs = []m.Ref{{Fields: []string{"video_id"}, Target: "videos"}}
		return e
	}
	full := []m.Field{
		col("detection_id", m.TypeInt),
		col("video_id", m.TypeInt),
		col("ts_start", m.TypeInt),
		col("ts_end", m.TypeInt),
	}
	cases := map[string]struct {
		e    m.Entity
		want []string
	}{
		"keyed by source and location": {
			e: refTo(detections(nil, [][]string{{"video_id", "ts_start", "ts_end"}}, full...)),
		},
		"no key over the location": {
			e:    refTo(detections(nil, nil, full...)),
			want: []string{"4.6-A detections"},
		},
		"by file name": {
			e: detections(
				nil,
				nil,
				col("detection_id", m.TypeInt),
				col("video_filename", m.TypeString),
				col("label", m.TypeString),
			),
			want: []string{"4.6-A detections"},
		},
		"declared, no location": {
			e: refTo(
				entityOf(
					"labels",
					[]string{"derived_from", "videos"},
					col("label_id", m.TypeInt),
					col("video_id", m.TypeInt),
				),
			),
			want: []string{"4.6-A labels"},
		},
		"declared, no source id": {
			e: entityOf(
				"labels",
				[]string{"derived_from", "videos"},
				col("label_id", m.TypeInt),
				col("page", m.TypeInt),
			),
			want: []string{"4.6-A labels"},
		},
		"name alone is a note": {
			e:    entityOf("segments", nil, col("segment_id", m.TypeInt)),
			want: []string{"4.6-A segments"},
		},
		"neither signal": {e: entityOf("labels", nil, col("label_id", m.TypeInt))},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := &m.Input{Sources: []m.Source{{Entities: []m.Entity{videos, tc.e}}}}
			equals(t, tc.want, findingsOf(in, "4.6-A"))
		})
	}
}

func TestResolutionOwnerInCodeowners(t *testing.T) {
	t.Parallel()
	customers := entityOf(
		"customers",
		[]string{"resolution", "customer_map", "resolution_owner", "@acme/sales"},
		col("customer_id", m.TypeInt),
		col("crm_id", m.TypeString),
		col("stripe_id", m.TypeString),
	)
	idMap := table(
		"customer_map",
		nil,
		col("customer_id", m.TypeInt),
		col("source_system", m.TypeString),
		col("source_id", m.TypeString),
	)
	run := func(codeowners string) []string {
		return findingsOf(&m.Input{
			Sources:   []m.Source{{Entities: []m.Entity{customers, idMap}}},
			Artifacts: m.Artifacts{Codeowners: m.ParseCodeowners(codeowners)},
		}, "4.2-A")
	}
	equals(t, []string(nil), run("db/ @acme/sales"))
	equals(t, []string{"4.2-A customers"}, run("db/ @acme/data"))
}

func TestLocationInKeyContract(t *testing.T) {
	t.Parallel()
	videoRef := m.Ref{Fields: []string{"video_id"}, Target: "videos"}
	fields := []m.Field{
		col("video_id", m.TypeInt), col("ts_start", m.TypeInt), col("ts_end", m.TypeInt),
		col("label", m.TypeString),
	}
	cases := map[string]struct {
		fileKV  []string
		kv      []string
		pk      []string
		uniques [][]string
		want    []string
	}{
		"location declared and keyed": {
			fileKV: []string{"layer", "unstructured"},
			kv:     []string{"location", "ts_start, ts_end"},
			pk:     []string{"video_id", "ts_start", "ts_end"},
		},
		"keyed by a UNIQUE constraint": {
			fileKV:  []string{"role", "file"},
			kv:      []string{"location", "ts_start,ts_end"},
			pk:      []string{"label"},
			uniques: [][]string{{"video_id", "ts_start", "ts_end"}},
		},
		"a whole-file description opts out": {
			fileKV: []string{"layer", "unstructured"},
			kv:     []string{"location", "whole_file"},
		},
		"no location declared": {
			fileKV: []string{"layer", "unstructured"},
			pk:     []string{"video_id", "ts_start"},
			want:   []string{"4.6-B detections"},
		},
		"a location field that does not exist": {
			fileKV: []string{"layer", "unstructured"},
			kv:     []string{"location", "frame"},
			pk:     []string{"video_id", "frame"},
			want:   []string{"4.6-B detections"},
		},
		"a location outside every key": {
			fileKV: []string{"layer", "unstructured"},
			kv:     []string{"location", "ts_start"},
			pk:     []string{"video_id", "label"},
			want:   []string{"4.6-B detections"},
		},
		"a parent that is not declared a file": {pk: []string{"video_id"}},
	}
	// Each case's detections also references an entity outside the catalog,
	// and the videos table, first, so neither may stop the check.
	otherRefs := []m.Ref{
		{Fields: []string{"label"}, Target: "labels_elsewhere"},
		{Fields: []string{"video_id"}, Target: "detections"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			videos := entityOf("videos", tc.fileKV, col("video_id", m.TypeInt))
			videos.PrimaryKey = []string{"video_id"}
			d := entityOf("detections", tc.kv, fields...)
			d.PrimaryKey, d.Uniques = tc.pk, tc.uniques
			d.Refs = append(slices.Clone(otherRefs), videoRef, videoRef)
			equals(t, tc.want, findingsOf(&m.Input{Sources: []m.Source{{
				Entities: []m.Entity{videos, d},
			}}}, "4.6-B"))
		})
	}
}

// TestDerivedEntityReadsDeclaredLocation checks that 4.6-A takes the
// location fields location= declares rather than guessing them from names.
func TestDerivedEntityReadsDeclaredLocation(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		kv      []string
		located bool
		want    []string
	}{
		"declared fields that no name pattern matches": {
			kv: []string{"derived_from", "videos", "location", "shot"},
		},
		"undeclared, so the names are guessed and none match": {
			kv: []string{"derived_from", "videos"}, want: []string{"4.6-A clips"},
		},
		"whole_file falls back to the location names": {
			kv: []string{"derived_from", "videos", "location", "whole_file"}, located: true,
		},
		"whole_file is not a location to key on": {
			kv:   []string{"derived_from", "videos", "location", "whole_file"},
			want: []string{"4.6-A clips"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			videos := table("videos", []string{"video_id"}, col("video_id", m.TypeInt))
			clips := entityOf("clips", tc.kv, col("video_id", m.TypeInt), col("shot", m.TypeInt))
			clips.PrimaryKey = []string{"video_id", "shot"}
			if tc.located {
				clips.Fields = append(clips.Fields, col("ts_start", m.TypeInt))
				clips.PrimaryKey = []string{"video_id", "ts_start"}
			}
			equals(t, tc.want, findingsOf(&m.Input{Sources: []m.Source{{
				Entities: []m.Entity{videos, clips},
			}}}, "4.6-A"))
		})
	}
}
