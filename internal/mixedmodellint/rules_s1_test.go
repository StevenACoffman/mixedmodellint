package mixedmodellint_test

import (
	"testing"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

const goodDefinition = "A request from a customer to buy goods, placed at checkout and fulfilled once."

func governedOrders(kv ...string) m.Entity {
	return entityOf("orders", kv, col("order_id", m.TypeInt))
}

func TestDefinitionContract(t *testing.T) {
	t.Parallel()
	full := []string{"definition", goodDefinition, "owner", "team:orders", "escalation", "role:cto"}
	with := func(k, v string) []string {
		out := append([]string(nil), full...)
		for i := 0; i < len(out); i += 2 {
			if out[i] == k {
				out[i+1] = v
				return out
			}
		}
		return append(out, k, v)
	}
	without := func(k string) []string {
		var out []string
		for i := 0; i < len(full); i += 2 {
			if full[i] != k {
				out = append(out, full[i], full[i+1])
			}
		}
		return out
	}
	cases := map[string]struct {
		kv   []string
		want []string
	}{
		"complete": {kv: full},
		"no definition": {
			kv:   without("definition"),
			want: []string{"1.2-A orders"},
		},
		"no owner": {kv: without("owner"), want: []string{"1.2-A orders"}},
		"no escalation": {
			kv:   without("escalation"),
			want: []string{"1.2-A orders"},
		},
		"short definition": {
			kv:   with("definition", "An order placed by a customer."),
			want: []string{"1.2-A orders"},
		},
		"definition only of its own name": {
			kv:   with("definition", "The orders of an order, for the order of the orders."),
			want: []string{"1.2-A orders"},
		},
		"placeholder definition": {
			kv:   with("definition", "TODO: write what an order is, who places it and when."),
			want: []string{"1.2-A orders"},
		},
		"owner not a principal": {
			kv:   with("owner", "the orders team"),
			want: []string{"1.2-A orders"},
		},
		"CODEOWNERS principal": {kv: with("owner", "@acme/orders")},
		"CODEOWNERS user":      {kv: with("escalation", "@alice")},
		"escalation is the owner": {
			kv:   with("escalation", "team:orders"),
			want: []string{"1.2-A orders"},
		},
		"scratch work":     {kv: []string{"lifecycle", "scratch"}},
		"third-party data": {kv: []string{"source", "third_party"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := &m.Input{Sources: []m.Source{{Entities: []m.Entity{governedOrders(tc.kv...)}}}}
			equals(t, tc.want, findingsOf(in, "1.2-A"))
		})
	}
}

func TestStaleDefinition(t *testing.T) {
	t.Parallel()
	base := governedOrders(
		"definition",
		goodDefinition,
		"owner",
		"team:orders",
		"escalation",
		"role:cto",
	)
	r := m.Analyze(&m.Input{Sources: []m.Source{{Entities: []m.Entity{base}}}})
	equals(t, 0, countDesign(r, "1.2-A"))

	stale := governedOrders("definition", goodDefinition+" fp=000000000000", "owner", "team:orders",
		"escalation", "role:cto")
	r = m.Analyze(&m.Input{Sources: []m.Source{{Entities: []m.Entity{stale}}}})
	equals(t, 1, countDesign(r, "1.2-A"))
	var fp string
	for i := range r.Findings {
		if r.Findings[i].Design == "1.2-A" {
			fp = r.Findings[i].Suggestion
		}
	}
	current := governedOrders("definition", goodDefinition+" fp="+fp[len(fp)-13:len(fp)-1],
		"owner", "team:orders", "escalation", "role:cto")
	r = m.Analyze(&m.Input{Sources: []m.Source{{Entities: []m.Entity{current}}}})
	equals(t, 0, countDesign(r, "1.2-A"))
}

func countDesign(r m.Report, design string) int {
	n := 0
	for i := range r.Findings {
		if r.Findings[i].Design == design {
			n++
		}
	}
	return n
}

func TestOwnershipRecords(t *testing.T) {
	t.Parallel()
	good := m.OwnershipRecord{
		Owner: "team:orders", Model: []string{"order*"},
		Responsibilities: []string{"definitions", "keep the order data reconciled"},
		Decisions:        []m.Decision{{Name: "approve_breaking_change", WithoutSignoff: true}},
		GrantedBy:        "role:vp-data",
	}
	edit := func(f func(r *m.OwnershipRecord)) []m.OwnershipRecord {
		r := good
		f(&r)
		return []m.OwnershipRecord{r}
	}
	cases := map[string]struct {
		records []m.OwnershipRecord
		owner   string
		want    []string
	}{
		"complete record": {records: []m.OwnershipRecord{good}},
		"no record":       {want: []string{"1.13-A orders"}},
		"another owner's record": {
			records: []m.OwnershipRecord{good},
			owner:   "team:growth",
			want:    []string{"1.13-A orders"},
		},
		"no responsibilities": {
			records: edit(func(r *m.OwnershipRecord) { r.Responsibilities = nil }),
			want:    []string{"1.13-A orders"},
		},
		"vague responsibility": {
			records: edit(func(r *m.OwnershipRecord) { r.Responsibilities = []string{"stuff"} }),
			want:    []string{"1.13-A orders"},
		},
		"needs sign-off for everything": {
			records: edit(
				func(r *m.OwnershipRecord) { r.Decisions = []m.Decision{{Name: "deprecate"}} },
			),
			want: []string{"1.13-A orders"},
		},
		"nobody granted it": {
			records: edit(func(r *m.OwnershipRecord) { r.GrantedBy = "" }),
			want:    []string{"1.13-A orders"},
		},
		"does not cover it": {
			records: edit(func(r *m.OwnershipRecord) { r.Model = []string{"carts"} }),
			want:    []string{"1.13-A orders"},
		},
		"covered by ID": {
			records: edit(func(r *m.OwnershipRecord) { r.Model = []string{"sql:*"} }),
		},
		"declared liability": {
			records: edit(func(r *m.OwnershipRecord) { r.Liability = true }),
			want:    []string{"1.13-A orders"},
		},
		"owner matched loosely": {records: []m.OwnershipRecord{good}, owner: "TEAM:orders"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			owner := tc.owner
			if owner == "" {
				owner = "team:orders"
			}
			in := &m.Input{
				Sources:   []m.Source{{Entities: []m.Entity{governedOrders("owner", owner)}}},
				Artifacts: m.Artifacts{Ownership: tc.records},
			}
			equals(t, tc.want, findingsOf(in, "1.13-A"))
		})
	}
}

func TestLiabilityIsAWarning(t *testing.T) {
	t.Parallel()
	r := m.Analyze(&m.Input{
		Sources: []m.Source{{Entities: []m.Entity{governedOrders("owner", "team:orders")}}},
		Artifacts: m.Artifacts{
			Ownership: []m.OwnershipRecord{{Owner: "team:orders", Liability: true}},
		},
	})
	for i := range r.Findings {
		if r.Findings[i].Design == "1.13-A" {
			equals(t, m.SeverityWarning, r.Findings[i].Severity)
			return
		}
	}
	t.Fatal("no 1.13-A finding")
}

func TestParseCodeowners(t *testing.T) {
	t.Parallel()
	equals(t, []string{"@acme/data", "@alice", "ops@acme.example"}, m.ParseCodeowners(
		"# owners\n\n*.sql @acme/data @alice\n/docs/ @acme/data  # again\nmodel/ ops@acme.example\nlonely\n",
	))
	equals(t, []string{}, m.ParseCodeowners(""))
}

func TestCodeownersPrincipals(t *testing.T) {
	t.Parallel()
	def := func(owner string) m.Entity {
		return governedOrders(
			"definition",
			goodDefinition,
			"owner",
			owner,
			"escalation",
			"role:cto",
		)
	}
	cases := map[string]struct {
		owner      string
		codeowners []string
		want       []string
	}{
		"listed": {owner: "@acme/orders", codeowners: []string{"@acme/orders"}},
		"not listed": {
			owner:      "@acme/orders",
			codeowners: []string{"@acme/data"},
			want:       []string{"1.2-A orders"},
		},
		"no CODEOWNERS to check":  {owner: "@acme/orders"},
		"a team needs no listing": {owner: "team:orders", codeowners: []string{"@acme/data"}},
		"an empty CODEOWNERS file": {
			owner:      "@acme/orders",
			codeowners: []string{},
			want:       []string{"1.2-A orders"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := &m.Input{
				Sources:   []m.Source{{Entities: []m.Entity{def(tc.owner)}}},
				Artifacts: m.Artifacts{Codeowners: tc.codeowners},
			}
			equals(t, tc.want, findingsOf(in, "1.2-A"))
		})
	}
}
