package mixedmodellint_test

import (
	"slices"
	"strings"
	"testing"

	"pgregory.net/rapid"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// enumField builds a string field constrained to values, with annotations kv.
func enumField(name string, values []string, kv ...string) m.Field {
	f := fieldWith(name, m.TypeString, kv...)
	f.Enum = values
	return f
}

func TestStandardValueSets(t *testing.T) {
	t.Parallel()
	shipments := func(fields ...m.Field) m.Entity {
		return table("shipments", []string{"shipment_id"},
			append([]m.Field{col("shipment_id", m.TypeInt)}, fields...)...)
	}
	cases := map[string]struct {
		field m.Field
		want  []string
	}{
		"country codes with no standard declared": {
			field: enumField("destination", []string{"US", "GB", "FR", "DE"}),
			want:  []string{"2.7-B shipments destination"},
		},
		"an off-standard variant": {
			field: enumField("destination", []string{"US", "UK", "FR", "DE"},
				"standard", "ISO 3166-1 alpha-2"),
			want: []string{"2.7-B shipments destination"},
		},
		"declared and all standard": {
			field: enumField("destination", []string{"US", "GB", "FR", "DE"},
				"standard", "ISO 3166-1 alpha-2"),
		},
		"declared local with a reason": {
			field: enumField("destination", []string{"US", "UK", "FR", "DE"},
				"standard", `local reason="the carrier's own codes"`),
		},
		"currency codes in lower case": {
			field: enumField("charged_in", []string{"usd", "eur", "gbp"}),
			want:  []string{"2.7-B shipments charged_in"},
		},
		"too few values to tell": {
			field: enumField("destination", []string{"US", "GB"}),
		},
		"mostly home-grown values": {
			field: enumField("speed", []string{"standard", "express", "overnight"}),
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.want, findingsOf(src(shipments(tc.field)), "2.7-B"))
		})
	}
}

// TestStandardValueSetsIgnoreOrderAndCase checks that 2.7-B judges a value
// set, not a list: shuffling the values or changing their case never
// changes whether the field is reported.
func TestStandardValueSetsIgnoreOrderAndCase(t *testing.T) {
	t.Parallel()
	pool := []string{"US", "GB", "UK", "FR", "DE", "JP", "EUR", "USD", "en", "fr", "XX", "foo"}
	rapid.Check(t, func(t *rapid.T) {
		values := rapid.SliceOfNDistinct(rapid.SampledFrom(pool), 3, 6,
			strings.ToUpper).Draw(t, "values")
		shuffled := rapid.Permutation(values).Draw(t, "shuffled")
		for i := range shuffled {
			if rapid.Bool().Draw(t, "lower") {
				shuffled[i] = strings.ToLower(shuffled[i])
			}
		}
		report := func(vs []string) []string {
			e := table("t", nil, enumField("code", slices.Clone(vs)))
			return findingsOf(src(e), "2.7-B")
		}
		if a, b := report(values), report(shuffled); !slices.Equal(a, b) {
			t.Fatalf("%v gives %v but %v gives %v", values, a, shuffled, b)
		}
	})
}

func TestRegulatedConcepts(t *testing.T) {
	t.Parallel()
	countries := entityOf("countries", []string{"standard", "ISO 3166-1 alpha-2"},
		col("country_code", m.TypeString))
	countries.PrimaryKey = []string{"country_code"}
	patients := func(fields ...m.Field) m.Entity {
		return table("patients", []string{"patient_id"},
			append([]m.Field{col("patient_id", m.TypeInt)}, fields...)...)
	}
	cases := map[string]struct {
		entities []m.Entity
		want     []string
		severity m.Severity
	}{
		"a diagnosis code with no standard": {
			entities: []m.Entity{patients(col("diagnosis_code", m.TypeString))},
			want:     []string{"2.7-A patients diagnosis_code"},
			severity: m.SeverityNote,
		},
		"a description naming the concept promotes it": {
			entities: []m.Entity{patients(func() m.Field {
				f := col("diagnosis_code", m.TypeString)
				f.Doc = "The ICD code the clinician recorded at discharge."
				return f
			}())},
			want:     []string{"2.7-A patients diagnosis_code"},
			severity: m.SeverityWarning,
		},
		"a partial value-set match promotes it": {
			entities: []m.Entity{patients(
				enumField("country", []string{"US", "GB", "Scotland", "Wales"}))},
			want:     []string{"2.7-A patients country"},
			severity: m.SeverityWarning,
		},
		"a declared standard": {
			entities: []m.Entity{patients(fieldWith("diagnosis_code", m.TypeString,
				"standard", "ICD-10"))},
		},
		"a reference to a standard's table": {
			entities: []m.Entity{
				countries,
				referencing(new(patients(col("country_code", m.TypeString))),
					"country_code", "countries"),
			},
		},
		"the standard's own table": {entities: []m.Entity{countries}},
		"a close value-set match is 2.7-B's": {
			entities: []m.Entity{patients(
				enumField("country", []string{"US", "GB", "FR", "DE"}))},
		},
		"an unrelated name": {
			entities: []m.Entity{patients(col("nickname", m.TypeString))},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := src(tc.entities...)
			equals(t, tc.want, findingsOf(in, "2.7-A"))
			for _, f := range m.Analyze(in).Findings {
				if f.Design == "2.7-A" {
					equals(t, tc.severity, f.Severity)
				}
			}
		})
	}
}
