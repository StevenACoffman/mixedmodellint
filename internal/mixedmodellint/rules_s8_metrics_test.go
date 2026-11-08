package mixedmodellint_test

import (
	"testing"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

func TestNominalMeasures(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		entity m.Entity
		want   []string
		level  m.Severity
	}{
		"a reference declared additive": {
			entity: referencing(new(table("orders", nil,
				fieldWith("store_id", m.TypeInt, "measure", "additive"))), "store_id", "stores"),
			want: []string{"8.8-A orders store_id"}, level: m.SeverityWarning,
		},
		"a code named as one": {
			entity: table(
				"customers",
				nil,
				fieldWith("zip_code", m.TypeInt, "measure", "additive"),
			),
			want:  []string{"8.8-A customers zip_code"},
			level: m.SeverityNote,
		},
		"a coded scale": {
			entity: table("reviews", nil, m.Field{
				Name: "rating", Type: m.TypeInt,
				Enum:        []string{"1", "2", "3", "4", "5"},
				Annotations: ann("measure", "non_additive", "scale", "ordinal"),
			}),
		},
		"a quantity": {
			entity: table("orders", nil, fieldWith("quantity", m.TypeInt, "measure", "additive")),
		},
		"declared nominal": {
			entity: table("customers", nil, fieldWith("zip_code", m.TypeInt, "measure", "nominal")),
		},
		"an embedding with no pooling": {
			entity: table("chunks", nil, m.Field{
				Name: "embedding", Type: m.TypeUnknown,
				RawType: "vector(1536)",
			}),
			want: []string{"8.8-A chunks embedding"}, level: m.SeverityWarning,
		},
		"an embedding pooled within a topic": {
			entity: table("chunks", nil, m.Field{
				Name: "embedding", RawType: "vector(1536)",
				Annotations: ann("pooling", "same_topic(topic_id)"),
			}),
		},
		"a declared embedding pooled nowhere": {
			entity: table("chunks", nil, fieldWith("vec", m.TypeArray, "embedding", "true",
				"pooling", "none")),
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := src(tc.entity)
			equals(t, tc.want, findingsOf(in, "8.8-A"))
			for _, f := range m.Analyze(in).Findings {
				if f.Design == "8.8-A" {
					equals(t, tc.level, f.Severity)
				}
			}
		})
	}
}

func TestReconciliationContracts(t *testing.T) {
	t.Parallel()
	atomic := table("order_lines", nil,
		fieldWith("revenue_usd", m.TypeDecimal, "measure", "additive", "currency", "USD"),
		col("note", m.TypeString))
	daily := func(kv ...string) m.Entity {
		return entityOf("daily_revenue", []string{"key_metric", "true"},
			fieldWith("revenue_usd", m.TypeDecimal, append([]string{
				"measure", "additive",
				"currency", "USD",
			}, kv...)...))
	}
	cases := map[string]struct {
		entities []m.Entity
		want     []string
	}{
		"a valid declaration": {
			entities: []m.Entity{atomic, daily("reconciles_with", "order_lines.revenue_usd")},
		},
		"none declared": {
			entities: []m.Entity{atomic, daily()},
			want:     []string{"8.9-B daily_revenue revenue_usd"},
		},
		"an unknown entity": {
			entities: []m.Entity{atomic, daily("reconciles_with", "lines.revenue_usd")},
			want:     []string{"8.9-B daily_revenue revenue_usd"},
		},
		"an unknown field": {
			entities: []m.Entity{atomic, daily("reconciles_with", "order_lines.revenue")},
			want:     []string{"8.9-B daily_revenue revenue_usd"},
		},
		"a target that is not additive": {
			entities: []m.Entity{atomic, daily("reconciles_with", "order_lines.note")},
			want:     []string{"8.9-B daily_revenue revenue_usd"},
		},
		"another currency": {
			entities: []m.Entity{
				table("order_lines", nil, fieldWith("revenue_eur", m.TypeDecimal,
					"measure", "additive", "currency", "EUR")),
				daily("reconciles_with", "order_lines.revenue_eur"),
			},
			want: []string{"8.9-B daily_revenue revenue_usd"},
		},
		"not a key metric": {
			entities: []m.Entity{atomic, table("daily_revenue", nil,
				fieldWith("revenue_usd", m.TypeDecimal, "measure", "additive"))},
		},
		"a field marked as the key metric": {
			entities: []m.Entity{atomic, table("daily_revenue", nil,
				fieldWith("revenue_usd", m.TypeDecimal, "key_metric", "true"))},
			want: []string{"8.9-B daily_revenue revenue_usd"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.want, findingsOf(src(tc.entities...), "8.9-B"))
		})
	}
}
