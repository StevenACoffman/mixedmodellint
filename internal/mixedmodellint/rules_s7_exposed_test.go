package mixedmodellint_test

import (
	"testing"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

func TestAgentExposureContract(t *testing.T) {
	t.Parallel()
	exposed := func(name string, kv ...string) m.Entity {
		return entityOf(name, append([]string{"agent_exposed", "true"}, kv...),
			col("sensor_id", m.TypeInt), col("reading_hour", m.TypeTimestamptz))
	}
	daily := table("daily_readings", nil, col("sensor_id", m.TypeInt))
	cases := map[string]struct {
		entities []m.Entity
		want     []string
	}{
		"a full contract": {
			entities: []m.Entity{exposed("readings", "grain", "one row per sensor per hour",
				"rollups", "day, month")},
		},
		"no grain or rollups": {
			entities: []m.Entity{exposed("readings")},
			want:     []string{"7.6-A readings"},
		},
		"a roll-up as fine as the grain": {
			entities: []m.Entity{exposed("readings", "grain", "one row per sensor per hour",
				"rollups", "hour, day")},
			want: []string{"7.6-A readings"},
		},
		"a roll-up naming an aggregate entity": {
			entities: []m.Entity{daily, exposed("readings", "grain", "one row per sensor per hour",
				"rollups", "[daily_readings]")},
		},
		"a roll-up naming nothing": {
			entities: []m.Entity{exposed("readings", "grain", "one row per sensor per hour",
				"rollups", "weekly_readings")},
			want: []string{"7.6-A readings"},
		},
		"a chunk entity without its size": {
			entities: []m.Entity{exposed("doc_chunks", "grain", "one row per chunk",
				"rollups", "documents"), table("documents", nil, col("document_id", m.TypeInt))},
			want: []string{"7.6-A doc_chunks"},
		},
		"a chunk entity with size and overlap": {
			entities: []m.Entity{
				exposed("doc_chunks",
					"grain", "one row per 512-token chunk, 64 overlap", "rollups", "documents"),
				table("documents", nil, col("document_id", m.TypeInt)),
			},
		},
		"not exposed": {entities: []m.Entity{table("readings", nil, col("v", m.TypeInt))}},
		"exposure turned off": {
			entities: []m.Entity{entityOf("readings", []string{"agent_exposed", "false"},
				col("v", m.TypeInt))},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.want, findingsOf(src(tc.entities...), "7.6-A"))
		})
	}
}
