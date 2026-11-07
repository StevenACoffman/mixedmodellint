package mixedmodellint_test

import (
	"testing"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

func TestNullSkippingAggregates(t *testing.T) {
	t.Parallel()
	nullable := func(kv ...string) m.Field {
		f := fieldWith("discount", m.TypeDecimal, kv...)
		f.Nullable = true
		return f
	}
	agg := func(fn string, distinct bool) m.Query {
		return m.Query{
			Kind: m.QuerySelect, Relations: []m.Relation{{Name: "orders", Alias: "o"}},
			Aggregates: []m.Aggregate{{
				Func: fn, Distinct: distinct,
				Arg: m.ColumnRef{Relation: "o", Column: "discount"},
			}},
		}
	}
	cases := map[string]struct {
		field m.Field
		q     m.Query
		want  []string
		level m.Severity
	}{
		"a sum where NULL means zero": {
			field: nullable("null", "zero"), q: agg("sum", false),
			want: []string{"5.3-B orders discount"}, level: m.SeverityError,
		},
		"a count where NULL means zero": {
			field: nullable("null", "zero"), q: agg("count", false),
			want: []string{"5.3-B orders discount"}, level: m.SeverityError,
		},
		"an undeclared nullable column is ambiguous": {
			field: nullable(), q: agg("avg", false),
			want: []string{"5.3-B orders discount"}, level: m.SeverityWarning,
		},
		"a count of an undeclared column counts values, as asked": {
			field: nullable(), q: agg("count", false),
		},
		"skipping a missing value is the declared intent": {
			field: nullable("null", "missing"), q: agg("sum", false),
		},
		"not nullable": {field: col("discount", m.TypeDecimal), q: agg("sum", false)},
		"another aggregate of a zero column": {
			field: nullable("null", "zero"), q: agg("array_agg", false),
		},
		"a column the entity does not have": {
			field: nullable("null", "zero"),
			q: m.Query{
				Kind: m.QuerySelect, Relations: []m.Relation{{Name: "orders", Alias: "o"}},
				Aggregates: []m.Aggregate{{Func: "sum", Arg: m.ColumnRef{Column: "ghost"}}},
			},
		},
		"COUNT(*)": {
			field: nullable("null", "zero"),
			q: m.Query{
				Kind: m.QuerySelect, Relations: []m.Relation{{Name: "orders", Alias: "o"}},
				Aggregates: []m.Aggregate{{Func: "count", Star: true}},
			},
		},
		"a distinct count": {field: nullable("null", "zero"), q: agg("count", true)},
		"wrapped in COALESCE, so no bare column": {
			field: nullable("null", "zero"),
			q: m.Query{
				Kind: m.QuerySelect, Relations: []m.Relation{{Name: "orders", Alias: "o"}},
				Aggregates: []m.Aggregate{{Func: "sum"}},
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := &m.Input{Sources: []m.Source{{
				Entities: []m.Entity{table("orders", nil, tc.field)},
				Queries:  []m.Query{tc.q},
			}}}
			equals(t, tc.want, findingsOf(in, "5.3-B"))
			for _, f := range m.Analyze(in).Findings {
				if f.Design == "5.3-B" {
					equals(t, tc.level, f.Severity)
				}
			}
		})
	}
}
