package mixedmodellint_test

import (
	"testing"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

func TestSemiAdditiveSums(t *testing.T) {
	t.Parallel()
	declared := fieldWith("balance", m.TypeDecimal, "measure", "semi_additive", "over",
		"snapshot_date")
	sum := func(groupBy, filters []string) m.Query {
		q := m.Query{
			Kind: m.QuerySelect, Relations: []m.Relation{{Name: "balances", Alias: "b"}},
			Aggregates: []m.Aggregate{
				{Func: "avg", Arg: m.ColumnRef{Column: "balance"}},
				{Func: "sum", Arg: m.ColumnRef{Column: "ghost"}},
				{Func: "sum", Arg: m.ColumnRef{Column: "balance"}},
			},
		}
		for _, g := range groupBy {
			q.GroupBy = append(q.GroupBy, m.ColumnRef{Column: g})
		}
		for _, f := range filters {
			q.Filters = append(q.Filters, m.ColumnRef{Column: f})
		}
		return q
	}
	cases := map[string]struct {
		balance m.Field
		q       m.Query
		want    []string
		level   m.Severity
	}{
		"summed across dates": {
			balance: declared, q: sum([]string{"account_id"}, nil),
			want: []string{"8.3-B balances balance"}, level: m.SeverityError,
		},
		"grouped by date":    {balance: declared, q: sum([]string{"snapshot_date"}, nil)},
		"pinned to one date": {balance: declared, q: sum(nil, []string{"snapshot_date"})},
		"undeclared, named like a balance": {
			balance: col("balance", m.TypeDecimal), q: sum(nil, nil),
			want: []string{"8.3-B balances balance"}, level: m.SeverityWarning,
		},
		"declared additive": {
			balance: fieldWith("balance", m.TypeDecimal, "measure", "additive"), q: sum(nil, nil),
		},
		"undeclared and not named like a balance": {
			balance: col("amount", m.TypeDecimal), q: sum(nil, nil),
		},
		"semi-additive without over=": {
			balance: fieldWith("balance", m.TypeDecimal, "measure", "semi_additive"),
			q:       sum(nil, nil),
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e := table("balances", []string{"account_id", "snapshot_date"},
				col("account_id", m.TypeInt), col("snapshot_date", m.TypeDate), tc.balance)
			in := &m.Input{Sources: []m.Source{{Entities: []m.Entity{e}, Queries: []m.Query{tc.q}}}}
			equals(t, tc.want, findingsOf(in, "8.3-B"))
			// Without a time key there is no dimension to guess for an
			// undeclared balance.
			untimed := table("balances", []string{"account_id"}, col("account_id", m.TypeInt),
				col("snapshot_date", m.TypeDate), col("balance", m.TypeDecimal))
			equals(t, []string(nil), findingsOf(&m.Input{Sources: []m.Source{{
				Entities: []m.Entity{untimed}, Queries: []m.Query{sum(nil, nil)},
			}}}, "8.3-B"))
			for _, f := range m.Analyze(in).Findings {
				if f.Design == "8.3-B" {
					equals(t, tc.level, f.Severity)
				}
			}
		})
	}
}

func TestReaggregatedValues(t *testing.T) {
	t.Parallel()
	agg := func(fn, column string) m.Query {
		return m.Query{
			Kind: m.QuerySelect, Relations: []m.Relation{{Name: "daily_stats", Alias: "d"}},
			Aggregates: []m.Aggregate{{Func: fn, Arg: m.ColumnRef{Column: column}}},
		}
	}
	cases := map[string]struct {
		q     m.Query
		want  []string
		level m.Severity
	}{
		"an average of a declared non-additive value": {
			q: agg("avg", "conversion"), want: []string{"8.4-B daily_stats conversion"},
			level: m.SeverityError,
		},
		"a sum of a value named as an average": {
			q: agg("sum", "avg_basket"), want: []string{"8.4-B daily_stats avg_basket"},
			level: m.SeverityWarning,
		},
		"a sum of an additive value":                    {q: agg("sum", "orders")},
		"a maximum of an average is ok":                 {q: agg("max", "avg_basket")},
		"a declared measure overrides the name":         {q: agg("sum", "avg_items")},
		"a column the entity does not have":             {q: agg("sum", "ghost")},
		"a count of an average is not a re-aggregation": {q: agg("count", "avg_basket")},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e := table("daily_stats", nil,
				fieldWith("conversion", m.TypeDecimal, "measure", "non_additive"),
				col("avg_basket", m.TypeDecimal),
				fieldWith("orders", m.TypeInt, "measure", "additive"),
				fieldWith("avg_items", m.TypeDecimal, "measure", "additive"))
			in := &m.Input{Sources: []m.Source{{Entities: []m.Entity{e}, Queries: []m.Query{tc.q}}}}
			equals(t, tc.want, findingsOf(in, "8.4-B"))
			for _, f := range m.Analyze(in).Findings {
				if f.Design == "8.4-B" {
					equals(t, tc.level, f.Severity)
				}
			}
		})
	}
}
