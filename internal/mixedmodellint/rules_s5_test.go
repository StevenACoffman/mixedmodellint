package mixedmodellint_test

import (
	"testing"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

func TestTimeColumnEventNames(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		field m.Field
		want  []string
	}{
		"a regular participle":          {field: col("shipped_at", m.TypeTimestamptz)},
		"a listed irregular participle": {field: col("paid_at", m.TypeTimestamptz)},
		"a reopened ticket, from the rule's example": {
			field: col("reopened_at", m.TypeTimestamptz),
		},
		"an event noun":        {field: col("first_response_at", m.TypeTimestamptz)},
		"a base verb":          {field: col("expires_on", m.TypeDate)},
		"a Go field name":      {field: col("CreatedAt", m.TypeTimestamptz)},
		"a period bound":       {field: col("period_start", m.TypeDate)},
		"a validity bound":     {field: col("valid_to", m.TypeTimestamptz)},
		"a non-temporal field": {field: col("status", m.TypeString)},
		"a declared time role": {
			field: fieldWith("eta", m.TypeTimestamptz, "time", "event"),
		},
		"a bare type word is 5.1-A's": {field: col("date", m.TypeDate)},
		"a numbered date": {
			field: col("order_date2", m.TypeDate), want: []string{"5.1-B items order_date2"},
		},
		"no suffix": {
			field: col("expiry", m.TypeDate), want: []string{"5.1-B items expiry"},
		},
		"a suffix after a word that is not an event": {
			field: col("unit_at", m.TypeTimestamptz), want: []string{"5.1-B items unit_at"},
		},
		"an event noun from the default list": {field: col("order_at", m.TypeTimestamptz)},
		"a stem that names no event": {
			field: col("color_ts", m.TypeTimestamptz), want: []string{"5.1-B items color_ts"},
		},
		"a four-letter -ed word is": {field: col("used_at", m.TypeTimestamptz)},
		"a short -ed word is not a participle": {
			field: col("red_at", m.TypeTimestamptz), want: []string{"5.1-B items red_at"},
		},
		"a time of day": {
			field: col("opening_time", m.TypeTime), want: []string{"5.1-B items opening_time"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e := table("items", []string{"item_id"}, col("item_id", m.TypeInt), tc.field)
			equals(t, tc.want, findingsOf(&m.Input{Sources: []m.Source{{
				Entities: []m.Entity{e},
			}}}, "5.1-B"))
		})
	}
}

func TestThirdPartyTimeNamesAreExempt(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		source string
		want   []string
	}{
		"owned":       {source: "owned", want: []string{"5.1-B vendor_rows color_ts"}},
		"third party": {source: "third_party"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e := entityOf("vendor_rows", []string{"source", tc.source},
				col("color_ts", m.TypeTimestamptz))
			// A third-party entity first must not stop the check.
			vendor := entityOf("vendor_feed", []string{"source", "third_party"},
				col("color_ts", m.TypeTimestamptz))
			equals(t, tc.want, findingsOf(&m.Input{Sources: []m.Source{{
				Entities: []m.Entity{vendor, e},
			}}}, "5.1-B"))
		})
	}
}
