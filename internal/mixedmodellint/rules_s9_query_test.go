package mixedmodellint_test

import (
	"testing"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

func TestReportingOnSystemTime(t *testing.T) {
	t.Parallel()
	report := func(groupBy, filter, trunc string) m.Query {
		q := m.Query{
			Kind: m.QuerySelect, Pos: m.Position{File: "report.sql", Line: 1},
			Relations:  []m.Relation{{Name: "clicks", Alias: "c"}},
			Aggregates: []m.Aggregate{{Func: "count", Star: true}},
		}
		if groupBy != "" {
			q.GroupBy = []m.ColumnRef{{Column: groupBy}}
		}
		if filter != "" {
			q.Filters = []m.ColumnRef{{Column: filter}}
		}
		if trunc != "" {
			q.TimeExprs = []m.TimeExpr{{
				Func: "date_trunc", Unit: "day",
				Column: m.ColumnRef{Column: trunc},
			}}
		}
		return q
	}
	cases := map[string]struct {
		q       m.Query
		noEvent bool
		want    []string
	}{
		"bucketed by load time": {
			q: report("", "", "loaded_at"), want: []string{"9.1-B clicks loaded_at"},
		},
		"grouped and filtered by load time, reported once": {
			q: report("loaded_at", "loaded_at", ""), want: []string{"9.1-B clicks loaded_at"},
		},
		"filtered by processing time": {
			q: report("", "processed_at", ""), want: []string{"9.1-B clicks processed_at"},
		},
		"bucketed by event time": {q: report("", "", "clicked_at")},
		"no event time to use instead": {
			q: report("", "", "loaded_at"), noEvent: true,
		},
		"grouped without an aggregate is still a report": {
			q: m.Query{
				Kind: m.QuerySelect, Relations: []m.Relation{{Name: "clicks", Alias: "c"}},
				GroupBy: []m.ColumnRef{{Column: "loaded_at"}},
			},
			want: []string{"9.1-B clicks loaded_at"},
		},
		"unknown and event columns before the load time": {
			q: func() m.Query {
				q := report("", "", "")
				q.GroupBy = []m.ColumnRef{
					{Column: "ghost"},
					{Column: "clicked_at"},
					{Column: "loaded_at"},
				}
				return q
			}(),
			want: []string{"9.1-B clicks loaded_at"},
		},
		"a load time of an entity without event time, then one with": {
			q: m.Query{
				Kind: m.QuerySelect,
				Relations: []m.Relation{
					{Name: "loads", Alias: "l"},
					{Name: "clicks", Alias: "c"},
				},
				Aggregates: []m.Aggregate{{Func: "count", Star: true}},
				GroupBy: []m.ColumnRef{
					{Relation: "l", Column: "loaded_at"},
					{Relation: "c", Column: "loaded_at"},
				},
			},
			want: []string{"9.1-B clicks loaded_at"},
		},
		"a load time stored as a string is 9.2's": {
			q: report("label_loaded_at", "", ""),
		},
		"not an aggregation": {
			q: m.Query{
				Kind: m.QuerySelect, Relations: []m.Relation{{Name: "clicks"}},
				Filters: []m.ColumnRef{{Column: "loaded_at"}},
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fields := []m.Field{
				col("loaded_at", m.TypeTimestamptz),
				col("processed_at", m.TypeTimestamptz),
			}
			if !tc.noEvent {
				fields = append(fields, col("clicked_at", m.TypeTimestamptz))
			}
			loads := table("loads", nil, col("loaded_at", m.TypeTimestamptz))
			plain := m.Query{Kind: m.QuerySelect, Relations: []m.Relation{{Name: "clicks"}}}
			in := &m.Input{Sources: []m.Source{{
				Entities: []m.Entity{loads, table("clicks", nil, fields...)},
				Queries:  []m.Query{plain, tc.q},
			}}}
			equals(t, tc.want, findingsOf(in, "9.1-B"))
		})
	}
}
