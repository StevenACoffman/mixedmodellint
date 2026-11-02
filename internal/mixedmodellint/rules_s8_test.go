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
