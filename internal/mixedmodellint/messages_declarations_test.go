package mixedmodellint_test

import (
	"testing"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// declarationMessageCases covers the single-entity contract designs 1.2-C,
// 4.6-B, 5.1-B, 8.5-B, 8.6-A, 9.2-B and 9.3-B: one input per message
// variant.
func declarationMessageCases(t *testing.T) map[string]messageCase {
	t.Helper()
	videos := placed("videos.sql", new(entityOf("videos", []string{"layer", "unstructured"},
		col("video_id", m.TypeInt))))
	videos.PrimaryKey = []string{"video_id"}
	detections := func(kv ...string) m.Entity {
		d := placed("detections.sql", new(entityOf("detections", kv,
			col("video_id", m.TypeInt), col("ts_start", m.TypeInt), col("label", m.TypeString))))
		d.PrimaryKey = []string{"video_id", "label"}
		d.Refs = []m.Ref{{
			Fields: []string{"video_id"}, Target: "videos",
			Pos: m.Position{File: "detections.sql", Line: 5},
		}}
		return d
	}
	sales := func(kv ...string) m.Entity {
		e := placed("sales.sql", new(entityOf("store_sales", kv, col("store_id", m.TypeInt),
			col("sale_date", m.TypeDate), col("revenue", m.TypeDecimal))))
		e.PrimaryKey = []string{"store_id"}
		return e
	}
	window := func(kv ...string) m.Entity {
		return placed("clicks_5m.sql", new(entityOf("clicks_5m", kv,
			col("window_start", m.TypeTimestamptz), col("window_end", m.TypeTimestamptz),
			fieldWith("clicks", m.TypeInt, "measure", "additive"))))
	}
	owned := func(codeowners string) *m.Input {
		e := placed("db/orders.sql", new(table("orders", nil, col("order_id", m.TypeInt))))
		e.Annotations = annAt(m.Position{File: "mml.yaml", Line: 4}, "owner", "team:orders")
		in := src(e)
		in.Artifacts.Codeowners = m.ParseCodeowners(codeowners)
		return in
	}
	return map[string]messageCase{
		"1.2-C elsewhere": {in: owned("/db/ @acme/data @bob"), designs: []string{"1.2-C"}},
		"1.2-C uncovered": {in: owned("/docs/ @acme/orders"), designs: []string{"1.2-C"}},
		"1.2-C user": {
			in: func() *m.Input {
				in := owned("/db/ @bob")
				in.Sources[0].Entities[0].Annotations = annAt(m.Position{File: "mml.yaml", Line: 4},
					"owner", "@alice")
				return in
			}(),
			designs: []string{"1.2-C"},
		},
		"1.2-C no team to copy": {in: owned("/db/ @bob"), designs: []string{"1.2-C"}},
		"1.2-C unowned":         {in: owned("* @acme/orders\n/db/"), designs: []string{"1.2-C"}},
		"4.6-B no location":     {in: src(videos, detections()), designs: []string{"4.6-B"}},
		"4.6-B missing field": {
			in: src(videos, detections("location", "frame")), designs: []string{"4.6-B"},
		},
		"4.6-B not keyed": {
			in: src(videos, detections("location", "ts_start")), designs: []string{"4.6-B"},
		},
		"5.1-B no suffix": {
			in:      src(placed("items.sql", new(table("items", nil, col("expiry", m.TypeDate))))),
			designs: []string{"5.1-B"},
		},
		"5.1-B time of day": {
			in: src(
				placed("items.sql", new(table("items", nil, col("shade_time", m.TypeTime)))),
			),
			designs: []string{"5.1-B"},
		},
		"5.1-B bare suffix": {
			in: src(
				placed("items.sql", new(table("items", nil, col("ts", m.TypeTimestamptz)))),
			),
			designs: []string{"5.1-B"},
		},
		"5.1-B no event": {
			in: src(placed("items.sql", new(table("items", nil,
				col("color_ts", m.TypeTimestamptz), col("open_time", m.TypeTime))))),
			designs: []string{"5.1-B"},
		},
		"8.5-B no period": {
			in:      src(sales("layer", "analytical", "dimensions", "store_id")),
			designs: []string{"8.5-B"},
		},
		"8.5-B dimensions": {
			in: src(func() m.Entity {
				e := sales("grain", "one row per store per day", "dimensions", "region")
				e.PrimaryKey = []string{"store_id", "sale_date"}
				return e
			}()),
			designs: []string{"8.5-B"},
		},
		"8.5-B no key": {
			in: src(func() m.Entity {
				e := sales("layer", "analytical")
				e.PrimaryKey = nil
				return e
			}()),
			designs: []string{"8.5-B"},
		},
		"8.5-B all time": {
			in:      src(sales("layer", "analytical", "period", "all_time")),
			designs: []string{"8.5-B"},
		},
		"8.6-A undeclared": {in: src(window()), designs: []string{"8.6-A"}},
		"8.6-A malformed sliding": {
			in:      src(window("window", "sliding(10m,5m)", "late_policy", "ignore")),
			designs: []string{"8.6-A"},
		},
		"8.6-A malformed window": {
			in:      src(window("window", "hopping", "lateness", "1m", "late_policy", "drop")),
			designs: []string{"8.6-A"},
		},
		"9.2-B no zone": {
			in: src(placed("events.sql", new(table("events", nil,
				col("local_start_at", m.TypeTimestampNaive))))),
			designs: []string{"9.2-B"},
		},
		"9.2-B unconstrained": {
			in: src(placed("events.sql", new(table("events", nil,
				col("local_start_at", m.TypeTimestampNaive), col("start_tz", m.TypeString))))),
			designs: []string{"9.2-B"},
		},
		"9.2-B abbreviation": {
			in: src(placed("events.sql", new(table("events", nil,
				col("local_start_at", m.TypeTimestampNaive),
				m.Field{Name: "start_tz", Type: m.TypeString, Enum: []string{"MST"}})))),
			designs: []string{"9.2-B"},
		},
		"9.3-B naive": {
			in: src(placed("stays.sql", new(table("stays", nil,
				col("valid_from", m.TypeTimestampNaive), col("valid_to", m.TypeTimestampNaive))))),
			designs: []string{"9.3-B"},
		},
		"9.3-B everything missing": {
			in: src(placed("stays.sql", new(table("stays", nil,
				col("valid_from", m.TypeDate),
				m.Field{Name: "valid_to", Type: m.TypeTimestamptz, Nullable: true})))),
			designs: []string{"9.3-B"},
		},
	}
}
