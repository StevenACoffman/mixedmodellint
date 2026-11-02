package mixedmodellint_test

import (
	"regexp"
	"testing"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

func TestStubs(t *testing.T) {
	t.Parallel()
	orders := table("orders", []string{"order_id"}, col("order_id", m.TypeInt))
	lines := table(
		"order_lines",
		[]string{"order_id", "line_no"},
		col("order_id", m.TypeInt),
		col("line_no", m.TypeInt),
	)
	lines.Refs = []m.Ref{{Fields: []string{"order_id"}, Target: "orders"}}
	nullableRef := table(
		"notes",
		[]string{"note_id"},
		col("note_id", m.TypeInt),
		col("order_id", m.TypeInt),
		col("guess_id", m.TypeInt),
	)
	nullableRef.Fields[1].Nullable, nullableRef.Fields[2].Nullable = true, true
	nullableRef.Refs = []m.Ref{
		{Fields: []string{"order_id"}, Target: "orders"},
		{Fields: []string{"guess_id"}, Target: "guesses", Inferred: true},
	}
	nullableRef.Annotations = ann(
		"grain",
		"one row per note",
		"definition",
		"x",
		"owner",
		"x",
		"escalation",
		"x",
	)
	sales := entityOf(
		"sales",
		[]string{"layer", "analytical", "grain", "one row per sale", "lifecycle", "scratch"},
		col("stock_level", m.TypeInt),
		fieldWith("units", m.TypeInt, "measure", "additive"),
	)
	keyless := entityOf("scratchpad", []string{"source", "third_party"}, col("x", m.TypeString))
	cases := map[string]struct {
		e    m.Entity
		want []m.Stub
	}{
		"a bare table": {e: orders, want: []m.Stub{
			{Entity: "order", Key: "definition", Value: "TODO"},
			{Entity: "order", Key: "escalation", Value: "TODO"},
			{
				Entity: "order",
				Key:    "grain",
				Value:  "one row per order fp=" + fingerprintOf(t, &orders),
			},
			{Entity: "order", Key: "owner", Value: "TODO"},
		}},
		"declared already; nullable fields": {e: nullableRef, want: []m.Stub{
			{Entity: "note", Field: "guess_id", Key: "null", Value: "TODO"},
			{Entity: "note", Field: "order_id", Key: "card", Value: "TODO"},
			{Entity: "note", Field: "order_id", Key: "null", Value: "TODO"},
		}},
		"measures, scratch work": {e: sales, want: []m.Stub{
			{
				Entity: "sale",
				Field:  "stock_level",
				Key:    "measure",
				Value:  "semi_additive over=time",
			},
		}},
		"no key, someone else's data": {e: keyless, want: []m.Stub{
			{Entity: "scratchpad", Key: "grain", Value: "TODO"},
		}},
		"a composite key": {e: lines, want: []m.Stub{
			{Entity: "order_line", Key: "definition", Value: "TODO"},
			{Entity: "order_line", Key: "escalation", Value: "TODO"},
			{
				Entity: "order_line",
				Key:    "grain",
				Value:  "one row per order line fp=" + fingerprintOf(t, &lines),
			},
			{Entity: "order_line", Key: "owner", Value: "TODO"},
		}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.want, m.Stubs(&m.Input{Sources: []m.Source{{Entities: []m.Entity{tc.e}}}}))
		})
	}
}

// fingerprintOf reads e's key fingerprint from the 7.1-A suggestion for a
// missing grain, the one public place it appears.
func fingerprintOf(t *testing.T, e *m.Entity) string {
	t.Helper()
	r := m.Analyze(&m.Input{Sources: []m.Source{{Entities: []m.Entity{*e}}}})
	rx := regexp.MustCompile(`fp=([0-9a-f]+)`)
	for i := range r.Findings {
		if f := &r.Findings[i]; f.Design == "7.1-A" && rx.MatchString(f.Suggestion) {
			return rx.FindStringSubmatch(f.Suggestion)[1]
		}
	}
	t.Fatalf("no 7.1-A fingerprint for %s", e.Name)
	return ""
}
