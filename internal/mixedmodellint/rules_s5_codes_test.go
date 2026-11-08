package mixedmodellint_test

import (
	"slices"
	"strings"
	"testing"

	"pgregory.net/rapid"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

func TestUnconstrainedCodes(t *testing.T) {
	t.Parallel()
	statuses := table("order_statuses", []string{"status"}, col("status", m.TypeString))
	cases := map[string]struct {
		entities []m.Entity
		want     []string
	}{
		"a status with no allowed list": {
			entities: []m.Entity{table("orders", nil, col("status", m.TypeString))},
			want:     []string{"5.4-A orders status"},
		},
		"a CHECK lists the values": {
			entities: []m.Entity{func() m.Entity {
				e := table("orders", nil, col("status", m.TypeString))
				e.Checks = []m.Check{{Field: "status", Enum: []string{"open", "closed"}}}
				return e
			}()},
		},
		"an enum lists the values": {
			entities: []m.Entity{table("orders", nil,
				enumField("return_reason", []string{"damaged", "late"}))},
		},
		"a reference to a lookup table": {
			entities: []m.Entity{
				statuses,
				referencing(new(table("orders", nil, col("status", m.TypeString))),
					"status", "order_statuses"),
			},
		},
		"a classification is 2.3-A's": {
			entities: []m.Entity{table("products", nil, col("category", m.TypeString))},
		},
		"a regulated code is 2.7-A's": {
			entities: []m.Entity{table("addresses", nil, col("country_code", m.TypeString))},
		},
		"a JSON property": {
			entities: []m.Entity{func() m.Entity {
				e := table("order", nil, col("paymentType", m.TypeString))
				e.Source = m.SourceJSONSchema
				return e
			}()},
			want: []string{"5.4-A order paymentType"},
		},
		"a Go field's constraint cannot be seen": {
			entities: []m.Entity{func() m.Entity {
				e := table("Order", nil, col("Status", m.TypeString))
				e.Source = m.SourceGo
				return e
			}()},
		},
		"an integer code": {entities: []m.Entity{table("orders", nil, col("status", m.TypeInt))}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.want, findingsOf(src(tc.entities...), "5.4-A"))
		})
	}
}

func TestCodeVariants(t *testing.T) {
	t.Parallel()
	orders := func(values ...string) m.Entity {
		return table("orders", nil, enumField("status", values))
	}
	jsonOrder := func(values ...string) m.Entity {
		e := table("order", nil, enumField("status", values))
		e.ID, e.Source = "json:order", m.SourceJSONSchema
		return e
	}
	cases := map[string]struct {
		entities []m.Entity
		want     []string
	}{
		"distinct codes": {entities: []m.Entity{orders("open", "shipped", "skipped", "closed")}},
		"one code in two cases": {
			entities: []m.Entity{orders("shipped", "SHIPPED ", "open")},
			want:     []string{"5.4-B orders status"},
		},
		"separators do not distinguish codes": {
			entities: []m.Entity{orders("on_hold", "on-hold")},
			want:     []string{"5.4-B orders status"},
		},
		"a prefix variant": {
			entities: []m.Entity{orders("defect", "defective", "late")},
			want:     []string{"5.4-B orders status"},
		},
		"a spelling variant": {
			entities: []m.Entity{orders("canceled", "cancelled")},
			want:     []string{"5.4-B orders status"},
		},
		"short codes are not compared for spelling": {
			entities: []m.Entity{orders("US", "UK", "low", "lo")},
		},
		"both kinds in one list": {
			entities: []m.Entity{orders("open", "Open", "defect", "defective")},
			want:     []string{"5.4-B orders status", "5.4-B orders status"},
		},
		"forms that agree": {
			entities: []m.Entity{orders("open", "closed"), jsonOrder("OPEN", "closed")},
		},
		"forms that disagree": {
			entities: []m.Entity{orders("open", "closed"), jsonOrder("open", "closed", "held")},
			want:     []string{"5.4-B orders status"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.want, findingsOf(src(tc.entities...), "5.4-B"))
		})
	}
}

// TestCodeVariantsIgnoreOrderAndCase checks that §5.4-B depends on the
// codes alone: listing the same values in another order, or all in another
// case, never changes what is reported.
func TestCodeVariantsIgnoreOrderAndCase(t *testing.T) {
	t.Parallel()
	pool := []string{
		"open", "opened", "shipped", "skipped", "closed", "cancelled", "canceled",
		"on_hold", "on-hold", "defect", "defective", "held",
	}
	rapid.Check(t, func(t *rapid.T) {
		values := rapid.SliceOfNDistinct(rapid.SampledFrom(pool), 2, 6,
			func(s string) string { return s }).Draw(t, "values")
		shuffled := rapid.Permutation(values).Draw(t, "shuffled")
		if rapid.Bool().Draw(t, "upper") {
			for i := range shuffled {
				shuffled[i] = strings.ToUpper(shuffled[i])
			}
		}
		report := func(vs []string) []string {
			return findingsOf(src(table("orders", nil, enumField("status", slices.Clone(vs)))),
				"5.4-B")
		}
		if a, b := report(values), report(shuffled); !slices.Equal(a, b) {
			t.Fatalf("%v gives %v but %v gives %v", values, a, shuffled, b)
		}
	})
}
