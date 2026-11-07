package mixedmodellint_test

import (
	"testing"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

func TestJunctionHazard(t *testing.T) {
	t.Parallel()
	orders := table(
		"orders",
		[]string{"order_id"},
		col("order_id", m.TypeInt),
		col("order_total", m.TypeDecimal),
	)
	declared := table("orders", []string{"order_id"}, col("order_id", m.TypeInt),
		fieldWith("net", m.TypeDecimal, "measure", "additive"))
	plain := table(
		"orders",
		[]string{"order_id"},
		col("order_id", m.TypeInt),
		col("note", m.TypeString),
	)
	tags := table("tags", []string{"tag_id"}, col("tag_id", m.TypeInt))
	link := func(kv []string, pk []string, extra ...m.Field) m.Entity {
		j := entityOf(
			"order_tags",
			kv,
			append([]m.Field{col("order_id", m.TypeInt), col("tag_id", m.TypeInt)}, extra...)...)
		j.PrimaryKey = pk
		j.Refs = []m.Ref{
			{Fields: []string{"order_id"}, Target: "orders"},
			{Fields: []string{"tag_id"}, Target: "tags"},
		}
		return j
	}
	wide := link(
		nil,
		[]string{"id"},
		col("id", m.TypeInt),
		col("a", m.TypeString),
		col("b", m.TypeString),
		col("c", m.TypeString),
	)
	cases := map[string]struct {
		x, j m.Entity
		want []string
	}{
		"junction keyed by both references": {
			x:    orders,
			j:    link(nil, []string{"order_id", "tag_id"}),
			want: []string{"8.2-A order_tags"},
		},
		"junction by its few other columns": {
			x:    orders,
			j:    link(nil, []string{"id"}, col("id", m.TypeInt)),
			want: []string{"8.2-A order_tags"},
		},
		"declared measure": {
			x:    declared,
			j:    link(nil, []string{"order_id", "tag_id"}),
			want: []string{"8.2-A order_tags"},
		},
		"no measure on either side": {
			x: plain,
			j: link(nil, []string{"order_id", "tag_id"}),
		},
		"policy declared": {
			x: orders,
			j: link([]string{"m2m", "overlapping"}, []string{"order_id", "tag_id"}),
		},
		"allocation column present": {
			x: orders,
			j: link(
				[]string{"m2m", "allocated(weight)"},
				[]string{"order_id", "tag_id"},
				col("weight", m.TypeDecimal),
			),
		},
		"allocation column missing": {
			x:    orders,
			j:    link([]string{"m2m", "allocated(weight)"}, []string{"order_id", "tag_id"}),
			want: []string{"8.2-A order_tags"},
		},
		"a wide entity is not a junction": {x: orders, j: wide},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := &m.Input{Sources: []m.Source{{Entities: []m.Entity{tc.x, tags, tc.j}}}}
			equals(t, tc.want, findingsOf(in, "8.2-A"))
		})
	}
}

func TestHeuristicMeasureLowersSeverity(t *testing.T) {
	t.Parallel()
	sev := func(x m.Entity) m.Severity {
		j := table(
			"order_tags",
			[]string{"order_id", "tag_id"},
			col("order_id", m.TypeInt),
			col("tag_id", m.TypeInt),
		)
		j.Refs = []m.Ref{
			{Fields: []string{"order_id"}, Target: "orders"},
			{Fields: []string{"tag_id"}, Target: "tags"},
		}
		r := m.Analyze(&m.Input{Sources: []m.Source{{Entities: []m.Entity{
			x, table("tags", []string{"tag_id"}, col("tag_id", m.TypeInt)), j,
		}}}})
		for i := range r.Findings {
			if r.Findings[i].Design == "8.2-A" {
				return r.Findings[i].Severity
			}
		}
		return 0
	}
	equals(
		t,
		m.SeverityWarning,
		sev(
			table(
				"orders",
				[]string{"order_id"},
				col("order_id", m.TypeInt),
				col("order_total", m.TypeDecimal),
			),
		),
	)
	equals(
		t,
		m.SeverityError,
		sev(
			table(
				"orders",
				[]string{"order_id"},
				col("order_id", m.TypeInt),
				fieldWith("net", m.TypeDecimal, "measure", "additive"),
			),
		),
	)
}

func TestMeasureClassification(t *testing.T) {
	t.Parallel()
	facts := func(f m.Field) m.Entity {
		e := entityOf("sales", []string{"layer", "analytical"}, col("sale_id", m.TypeInt), f)
		e.PrimaryKey = []string{"sale_id"}
		return e
	}
	cases := map[string]struct {
		e    m.Entity
		want []string
	}{
		"undeclared amount": {
			e:    facts(col("revenue", m.TypeDecimal)),
			want: []string{"8.3-A sales revenue"},
		},
		"declared additive": {
			e: facts(fieldWith("revenue", m.TypeDecimal, "measure", "additive")),
		},
		"declared with a reason": {
			e: facts(fieldWith("revenue", m.TypeDecimal, "measure", "additive net of refunds")),
		},
		"not a class": {
			e:    facts(fieldWith("revenue", m.TypeDecimal, "measure", "summable")),
			want: []string{"8.3-A sales revenue"},
		},
		"semi-additive without over": {
			e:    facts(fieldWith("balance", m.TypeDecimal, "measure", "semi_additive")),
			want: []string{"8.3-A sales balance"},
		},
		"semi-additive over time": {
			e: facts(
				fieldWith("balance", m.TypeDecimal, "measure", "semi_additive", "over", "time"),
			),
		},
		"contradicts the lexicon": {
			e:    facts(fieldWith("unit_price", m.TypeDecimal, "measure", "additive")),
			want: []string{"8.3-A sales unit_price"},
		},
		"no lexicon word, any class": {
			e: facts(fieldWith("units", m.TypeInt, "measure", "non_additive")),
		},
		"a reference id is not a number": {e: facts(col("store_id", m.TypeInt))},
		"text is not a measure":          {e: facts(col("label", m.TypeString))},
		"an enumerated code": {
			e: facts(m.Field{Name: "tier", Type: m.TypeInt, Enum: []string{"1", "2"}}),
		},
		"oltp without a grain": {
			e: entityOf("carts", []string{"layer", "oltp"}, col("revenue", m.TypeDecimal)),
		},
		"anything with a grain": {
			e: entityOf(
				"carts",
				[]string{"grain", "one row per cart"},
				col("revenue", m.TypeDecimal),
			),
			want: []string{"8.3-A carts revenue"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := &m.Input{Sources: []m.Source{{Entities: []m.Entity{tc.e}}}}
			equals(t, tc.want, findingsOf(in, "8.3-A"))
		})
	}
}

func TestMeasureContradictionIsANote(t *testing.T) {
	t.Parallel()
	e := entityOf("sales", []string{"layer", "analytical"},
		fieldWith("unit_price", m.TypeDecimal, "measure", "additive"))
	r := m.Analyze(&m.Input{Sources: []m.Source{{Entities: []m.Entity{e}}}})
	for i := range r.Findings {
		if r.Findings[i].Design == "8.3-A" {
			equals(t, m.SeverityNote, r.Findings[i].Severity)
			return
		}
	}
	t.Fatal("no 8.3-A finding")
}

func TestStoredAggregateBounds(t *testing.T) {
	t.Parallel()
	daily := "one row per store per day"
	storeCol, dateCol := col("store_id", m.TypeInt), col("sale_date", m.TypeDate)
	revenue := col("revenue", m.TypeDecimal)
	cases := map[string]struct {
		kv     []string
		pk     []string
		fields []m.Field
		refs   []m.Ref
		want   []string
	}{
		"a date in the key and declared dimensions": {
			kv: []string{"grain", daily, "dimensions", "store_id"},
			pk: []string{"store_id", "sale_date"}, fields: []m.Field{storeCol, dateCol, revenue},
		},
		"period bounds in the key": {
			kv: []string{"layer", "analytical", "dimensions", "store_id"},
			pk: []string{"store_id", "period_start", "period_end"},
			fields: []m.Field{
				storeCol, col("period_start", m.TypeInt),
				col("period_end", m.TypeInt), revenue,
			},
		},
		"a reference to dim_date in the key": {
			kv:     []string{"layer", "analytical", "dimensions", "store_id"},
			pk:     []string{"store_id", "date_key"},
			fields: []m.Field{storeCol, col("date_key", m.TypeInt), revenue},
			refs:   []m.Ref{{Fields: []string{"date_key"}, Target: "dim_date"}},
		},
		"no period in the key": {
			kv: []string{"layer", "analytical", "dimensions", "store_id"},
			pk: []string{"store_id"}, fields: []m.Field{storeCol, revenue},
			want: []string{"8.5-B store_sales"},
		},
		"a period outside the key": {
			kv: []string{"grain", daily, "dimensions", "store_id"},
			pk: []string{"store_id"}, fields: []m.Field{storeCol, dateCol, revenue},
			want: []string{"8.5-B store_sales"},
		},
		"no dimensions declared": {
			kv: []string{"grain", daily}, pk: []string{"store_id", "sale_date"},
			fields: []m.Field{storeCol, dateCol, revenue},
			want:   []string{"8.5-B store_sales"},
		},
		"a dimension that is not a key column": {
			kv:     []string{"grain", daily, "dimensions", "store_id, region"},
			pk:     []string{"store_id", "sale_date"},
			fields: []m.Field{storeCol, dateCol, col("region", m.TypeString), revenue},
			want:   []string{"8.5-B store_sales"},
		},
		"an all-time aggregate with as_of": {
			kv:     []string{"layer", "analytical", "period", "all_time", "dimensions", "store_id"},
			pk:     []string{"store_id"},
			fields: []m.Field{storeCol, col("as_of", m.TypeTimestamptz), revenue},
		},
		"an all-time aggregate without as_of": {
			kv: []string{"layer", "analytical", "period", "all_time", "dimensions", "store_id"},
			pk: []string{"store_id"}, fields: []m.Field{storeCol, revenue},
			want: []string{"8.5-B store_sales"},
		},
		"only one period bound in the key": {
			kv: []string{"layer", "analytical", "dimensions", "store_id"},
			pk: []string{"store_id", "period_start"},
			fields: []m.Field{
				storeCol, col("period_start", m.TypeInt),
				col("period_end", m.TypeInt), revenue,
			},
			want: []string{"8.5-B store_sales"},
		},
		"a reference to dim_date outside the key": {
			kv:     []string{"layer", "analytical", "dimensions", "store_id"},
			pk:     []string{"store_id"},
			fields: []m.Field{storeCol, col("date_key", m.TypeInt), revenue},
			refs:   []m.Ref{{Fields: []string{"date_key"}, Target: "dim_date"}},
			want:   []string{"8.5-B store_sales"},
		},
		"a key reference to another dimension": {
			kv:     []string{"layer", "analytical", "dimensions", "store_id"},
			pk:     []string{"store_id", "region_key"},
			fields: []m.Field{storeCol, col("region_key", m.TypeInt), revenue},
			refs:   []m.Ref{{Fields: []string{"region_key"}, Target: "dim_region"}},
			want:   []string{"8.5-B store_sales"},
		},
		"a key reference to a dim_date the catalog lacks": {
			kv:     []string{"layer", "analytical", "dimensions", "store_id"},
			pk:     []string{"store_id", "date_key"},
			fields: []m.Field{storeCol, col("date_key", m.TypeInt), revenue},
			refs:   []m.Ref{{Fields: []string{"date_key"}, Target: "dim_calendar"}},
			want:   []string{"8.5-B store_sales"},
		},
		"an atomic table is not an aggregate": {
			kv: []string{"grain", "one row per sale"}, pk: []string{"store_id"},
			fields: []m.Field{storeCol, revenue},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e := entityOf("store_sales", tc.kv, tc.fields...)
			e.PrimaryKey, e.Refs = tc.pk, tc.refs
			dimDate := table("dim_date", []string{"date_key"}, col("date_key", m.TypeInt))
			dimRegion := table("dim_region", []string{"region_key"}, col("region_key", m.TypeInt))
			equals(t, tc.want, findingsOf(&m.Input{Sources: []m.Source{{
				Entities: []m.Entity{dimDate, dimRegion, e},
			}}}, "8.5-B"))
		})
	}
}

func TestWindowOutputContract(t *testing.T) {
	t.Parallel()
	start, end := col("window_start", m.TypeTimestamptz), col("window_end", m.TypeTimestamptz)
	declared := []string{"window", "tumbling(5m)", "lateness", "10m", "late_policy", "update"}
	cases := map[string]struct {
		kv     []string
		fields []m.Field
		want   []string
	}{
		"a fully declared tumbling window": {kv: declared, fields: []m.Field{start, end}},
		"nothing declared": {
			fields: []m.Field{start, end}, want: []string{"8.6-A clicks_5m"},
		},
		"a malformed window": {
			kv:     []string{"window", "hopping", "lateness", "1m", "late_policy", "drop"},
			fields: []m.Field{start, end}, want: []string{"8.6-A clicks_5m"},
		},
		"an unknown late policy": {
			kv:     []string{"window", "session(30m)", "lateness", "1m", "late_policy", "ignore"},
			fields: []m.Field{start, end}, want: []string{"8.6-A clicks_5m"},
		},
		"no lateness": {
			kv:     []string{"window", "session(30m)", "late_policy", "side_output"},
			fields: []m.Field{start, end}, want: []string{"8.6-A clicks_5m"},
		},
		"a sliding window with a non-additive measure": {
			kv: []string{"window", "sliding(10m, 5m)", "lateness", "1m", "late_policy", "drop"},
			fields: []m.Field{
				start, end,
				fieldWith("clicks", m.TypeInt, "measure", "non_additive"),
			},
		},
		"a sliding window with an additive measure": {
			kv:     []string{"window", "sliding(10m,5m)", "lateness", "1m", "late_policy", "drop"},
			fields: []m.Field{start, end, fieldWith("clicks", m.TypeInt, "measure", "additive")},
			want:   []string{"8.6-A clicks_5m"},
		},
		"a tumbling window may add up": {
			kv:     declared,
			fields: []m.Field{start, end, fieldWith("clicks", m.TypeInt, "measure", "additive")},
		},
		"only a window start": {fields: []m.Field{start}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e := entityOf("clicks_5m", tc.kv, tc.fields...)
			plain := table("clicks", nil, col("clicked_at", m.TypeTimestamptz))
			equals(t, tc.want, findingsOf(&m.Input{Sources: []m.Source{{
				Entities: []m.Entity{plain, e},
			}}}, "8.6-A"))
		})
	}
}
