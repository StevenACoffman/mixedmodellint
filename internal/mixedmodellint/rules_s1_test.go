package mixedmodellint_test

import (
	"slices"
	"strings"
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
		codeowners *m.Codeowners
		want       []string
	}{
		"listed": {owner: "@acme/orders", codeowners: m.ParseCodeowners("* @acme/orders")},
		"not listed": {
			owner:      "@acme/orders",
			codeowners: m.ParseCodeowners("* @acme/data"),
			want:       []string{"1.2-A orders"},
		},
		"no CODEOWNERS to check": {owner: "@acme/orders"},
		"a team needs no listing": {
			owner: "team:orders", codeowners: m.ParseCodeowners("* @acme/data"),
		},
		"an empty CODEOWNERS file": {
			owner:      "@acme/orders",
			codeowners: m.ParseCodeowners(""),
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

func TestThinLookupChains(t *testing.T) {
	t.Parallel()
	// lookup is a table keyed by <name>_id with one attribute, referencing
	// parent when it is not empty.
	lookup := func(name, parent string, extra ...m.Field) m.Entity {
		fields := append([]m.Field{col(name+"_id", m.TypeInt), col("name", m.TypeString)},
			extra...)
		e := table(name, []string{name + "_id"}, fields...)
		if parent != "" {
			e.Fields = append(e.Fields, col(parent+"_id", m.TypeInt))
			e.Refs = []m.Ref{{Fields: []string{parent + "_id"}, Target: parent}}
		}
		return e
	}
	addresses := lookup("address", "city", col("street", m.TypeString),
		col("postcode", m.TypeString))
	cases := map[string]struct {
		entities []m.Entity
		want     []string
	}{
		"address → city → state → country": {
			entities: []m.Entity{
				addresses, lookup("city", "state"), lookup("state", "country"),
				lookup("country", ""),
			},
			want: []string{"1.6-B address"},
		},
		"a chain of two is fine": {
			entities: []m.Entity{addresses, lookup("city", "state"), lookup("state", "")},
		},
		"a lookup with real attributes breaks the chain": {
			entities: []m.Entity{
				addresses,
				lookup("city", "state", col("population", m.TypeInt), col("area", m.TypeInt)),
				lookup("state", "country"), lookup("country", ""),
			},
		},
		"audit columns do not count as attributes": {
			entities: []m.Entity{
				addresses,
				lookup("city", "state", col("created_at", m.TypeTimestamptz),
					col("updated_by", m.TypeString)),
				lookup("state", "country"), lookup("country", ""),
			},
			want: []string{"1.6-B address"},
		},
		"found whatever the order of the entities": {
			entities: []m.Entity{
				table("orders", []string{"order_id"}, col("order_id", m.TypeInt)),
				lookup("country", ""), lookup("state", "country"), lookup("city", "state"),
				addresses,
			},
			want: []string{"1.6-B address"},
		},
		"a link without a key is not a lookup": {
			entities: []m.Entity{
				addresses, lookup("city", "state"),
				func() m.Entity { e := lookup("state", "country"); e.PrimaryKey = nil; return e }(),
				lookup("country", ""),
			},
		},
		"a self-referencing hierarchy is one table, not a chain": {
			entities: []m.Entity{
				addresses, lookup("city", "state"),
				func() m.Entity {
					e := lookup("state", "country")
					e.Fields = append(e.Fields, col("parent_id", m.TypeInt))
					e.Refs = append(e.Refs, m.Ref{Fields: []string{"parent_id"}, Target: "state"})
					return e
				}(),
				lookup("country", ""),
			},
		},
		"two attributes are still thin": {
			entities: []m.Entity{
				addresses, lookup("city", "state"),
				lookup("state", "country"), lookup("country", "", col("iso", m.TypeString)),
			},
			want: []string{"1.6-B address"},
		},
		"three attributes are not": {
			entities: []m.Entity{
				addresses, lookup("city", "state"),
				lookup("state", "country"),
				lookup("country", "", col("iso", m.TypeString), col("flag", m.TypeString)),
			},
		},
		"a created_at audit column": {
			entities: []m.Entity{
				addresses,
				lookup("city", "state"),
				lookup("state", "country"),
				lookup(
					"country",
					"",
					col("iso", m.TypeString),
					col("created_at", m.TypeTimestamptz),
				),
			},
			want: []string{"1.6-B address"},
		},
		"an updated_at audit column": {
			entities: []m.Entity{
				addresses,
				lookup("city", "state"),
				lookup("state", "country"),
				lookup(
					"country",
					"",
					col("iso", m.TypeString),
					col("updated_at", m.TypeTimestamptz),
				),
			},
			want: []string{"1.6-B address"},
		},
		"a column in a UNIQUE key is not an attribute": {
			entities: []m.Entity{
				addresses, lookup("city", "state"),
				lookup("state", "country"),
				func() m.Entity {
					e := lookup("country", "", col("iso", m.TypeString), col("code", m.TypeString))
					e.Uniques = [][]string{{"code"}}
					return e
				}(),
			},
			want: []string{"1.6-B address"},
		},
		"the longer of two chains": {
			entities: []m.Entity{
				func() m.Entity {
					e := addresses
					e.Fields = append(slices.Clone(e.Fields), col("zone_id", m.TypeInt))
					e.Refs = append(
						slices.Clone(e.Refs),
						m.Ref{Fields: []string{"zone_id"}, Target: "zone"},
					)
					return e
				}(),
				lookup("zone", ""), lookup("city", "state"), lookup("state", "country"),
				lookup("country", ""),
			},
			want: []string{"1.6-B address"},
		},
		"a shared lookup is a taxonomy, not a chain link": {
			entities: []m.Entity{
				addresses, lookup("city", "state"), lookup("state", "country"),
				lookup("country", ""), lookup("airport", "country", col("iata", m.TypeString),
					col("icao", m.TypeString)),
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.want, findingsOf(&m.Input{Sources: []m.Source{{
				Entities: tc.entities,
			}}}, "1.6-B"))
		})
	}
}

func TestScratchModelsWithDependents(t *testing.T) {
	t.Parallel()
	schemaTable := func(schema, name string) m.Entity {
		e := table(name, nil, col("x", m.TypeInt))
		e.ID = "sql:" + schema + "." + name
		return e
	}
	readFrom := func(rel, target string, kind m.QueryKind) m.Query {
		return m.Query{
			Kind: kind, Target: target, Pos: m.Position{File: "report.sql", Line: 4},
			Relations: []m.Relation{{Name: rel, Alias: rel}},
		}
	}
	cases := map[string]struct {
		scratch m.Entity
		others  []m.Entity
		queries []m.Query
		want    []string
	}{
		"a scratch name read by a report": {
			scratch: table("tmp_orders", nil, col("order_id", m.TypeInt)),
			queries: []m.Query{readFrom("tmp_orders", "", m.QuerySelect)},
			want:    []string{"1.7-A tmp_orders"},
		},
		"a dated copy read by a view": {
			scratch: table("orders_20240101", nil, col("order_id", m.TypeInt)),
			queries: []m.Query{readFrom("orders_20240101", "order_report", m.QueryCreateView)},
			want:    []string{"1.7-A orders_20240101"},
		},
		"a scratch schema referenced by a table": {
			scratch: func() m.Entity {
				e := table("picks", []string{"pick_id"}, col("pick_id", m.TypeInt))
				e.ID = "sql:sandbox.picks"
				return e
			}(),
			others: []m.Entity{func() m.Entity {
				e := table("orders", []string{"order_id"}, col("pick_id", m.TypeInt))
				e.Refs = []m.Ref{{Fields: []string{"pick_id"}, Target: "sandbox.picks"}}
				return e
			}()},
			want: []string{"1.7-A picks"},
		},
		"declared scratch read only by scratch": {
			scratch: entityOf("picks", []string{"lifecycle", "scratch"}, col("x", m.TypeInt)),
			others:  []m.Entity{table("tmp_picks2", nil, col("x", m.TypeInt))},
			queries: []m.Query{readFrom("picks", "tmp_picks2", m.QueryInsert)},
		},
		"a declared lifecycle overrides the name": {
			scratch: entityOf("orders_v2", []string{"lifecycle", "recurring"},
				col("order_id", m.TypeInt)),
			queries: []m.Query{readFrom("orders_v2", "", m.QuerySelect)},
		},
		"written and dropped, never read": {
			scratch: table("tmp_load", nil, col("x", m.TypeInt)),
			queries: []m.Query{{Kind: m.QueryDrop, Target: "tmp_load"}},
		},
		"a clean name": {
			scratch: table("orders", nil, col("order_id", m.TypeInt)),
			queries: []m.Query{readFrom("orders", "", m.QuerySelect)},
		},
		"declared scratch read by a report": {
			scratch: entityOf("picks", []string{"lifecycle", "scratch"}, col("x", m.TypeInt)),
			queries: []m.Query{readFrom("picks", "", m.QuerySelect)},
			want:    []string{"1.7-A picks"},
		},
		"a scratch schema": {
			scratch: schemaTable("scratch", "picks"),
			queries: []m.Query{readFrom("picks", "", m.QuerySelect)},
			want:    []string{"1.7-A picks"},
		},
		"a tmp schema": {
			scratch: schemaTable("tmp", "picks"),
			queries: []m.Query{readFrom("picks", "", m.QuerySelect)},
			want:    []string{"1.7-A picks"},
		},
		"a dev_ schema": {
			scratch: schemaTable("dev_alice", "picks"),
			queries: []m.Query{readFrom("picks", "", m.QuerySelect)},
			want:    []string{"1.7-A picks"},
		},
		"an ordinary schema": {
			scratch: schemaTable("sales", "picks"),
			queries: []m.Query{readFrom("picks", "", m.QuerySelect)},
		},
		"a table named like a scratch schema is not in one": {
			scratch: table("tmp", nil, col("x", m.TypeInt)),
			queries: []m.Query{readFrom("tmp", "", m.QuerySelect)},
		},
		"a scratch table referencing itself": {
			scratch: func() m.Entity {
				e := table(
					"tmp_tree",
					[]string{"id"},
					col("id", m.TypeInt),
					col("parent_id", m.TypeInt),
				)
				e.Refs = []m.Ref{{Fields: []string{"parent_id"}, Target: "tmp_tree"}}
				return e
			}(),
		},
		"referenced only by another scratch table": {
			scratch: table("tmp_picks", []string{"pick_id"}, col("pick_id", m.TypeInt)),
			others: []m.Entity{func() m.Entity {
				e := table("tmp_orders", nil, col("pick_id", m.TypeInt))
				e.Refs = []m.Ref{{Fields: []string{"pick_id"}, Target: "tmp_picks"}}
				return e
			}()},
		},
		"read through a CTE of the same name is not a read": {
			scratch: table("tmp_orders", nil, col("order_id", m.TypeInt)),
			queries: []m.Query{{
				Kind:      m.QuerySelect,
				Relations: []m.Relation{{Name: "tmp_orders", Alias: "tmp_orders", Derived: true}},
			}},
		},
		"a query reading something else, then one reading it": {
			scratch: table("tmp_orders", nil, col("order_id", m.TypeInt)),
			others:  []m.Entity{table("orders", nil, col("order_id", m.TypeInt))},
			queries: []m.Query{
				readFrom("orders", "", m.QuerySelect),
				readFrom("tmp_orders", "tmp_copy", m.QueryInsert),
				readFrom("tmp_orders", "", m.QuerySelect),
			},
			want: []string{"1.7-A tmp_orders"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			clean := table("customers", nil, col("customer_id", m.TypeInt))
			equals(t, tc.want, findingsOf(&m.Input{Sources: []m.Source{{
				Entities: append([]m.Entity{clean, tc.scratch}, tc.others...),
				Queries:  tc.queries,
			}}}, "1.7-A"))
		})
	}
}

func TestCodeownersConsistency(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		owner      string
		codeowners *m.Codeowners
		want       []string
	}{
		"a team routed to its GitHub team": {
			owner: "team:orders", codeowners: m.ParseCodeowners("/db/ @acme/orders"),
		},
		"a group routed to its GitHub team": {
			owner: "group:orders", codeowners: m.ParseCodeowners("/db/ @acme/orders"),
		},
		"a principal routed to itself": {
			owner: "@acme/orders", codeowners: m.ParseCodeowners("*.sql @alice @acme/orders"),
		},
		"routed to someone else": {
			owner: "team:orders", codeowners: m.ParseCodeowners("/db/ @acme/data"),
			want: []string{"1.2-C orders"},
		},
		"the last rule decides": {
			owner:      "team:orders",
			codeowners: m.ParseCodeowners("/db/ @acme/orders\n*.sql @acme/data"),
			want:       []string{"1.2-C orders"},
		},
		"no rule covers the file": {
			owner: "team:orders", codeowners: m.ParseCodeowners("/docs/ @acme/orders"),
			want: []string{"1.2-C orders"},
		},
		"a user is not a team": {
			owner: "team:alice", codeowners: m.ParseCodeowners("/db/ @alice"),
			want: []string{"1.2-C orders"},
		},
		"no CODEOWNERS": {owner: "team:orders"},
		"a role has no CODEOWNERS form": {
			owner: "role:cto", codeowners: m.ParseCodeowners("/db/ @acme/data"),
		},
		"no owner is 1.2-A's": {codeowners: m.ParseCodeowners("/db/ @acme/data")},
		"an owner that is not a principal is 1.2-A's": {
			owner: "the orders team", codeowners: m.ParseCodeowners("/db/ @acme/data"),
		},
		"a team does not match a user of the same name": {
			owner: "team:orders", codeowners: m.ParseCodeowners("/db/ @orders"),
			want: []string{"1.2-C orders"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e := table("orders", []string{"order_id"}, col("order_id", m.TypeInt))
			e.Pos = m.Position{File: "db/orders.sql", Line: 1}
			if tc.owner != "" {
				e.Annotations = ann("owner", tc.owner)
			}
			// Entities that 1.2-C skips or passes come first, so a skip that
			// stopped the whole check would show.
			unplaced := table("carts", nil, col("cart_id", m.TypeInt))
			unplaced.Annotations, unplaced.Pos = ann("owner", "team:carts"), m.Position{}
			routed := table("customers", nil, col("customer_id", m.TypeInt))
			routed.Annotations = ann("owner", "@acme/sales")
			routed.Pos = m.Position{File: "sales/customers.sql", Line: 1}
			co := tc.codeowners
			if co != nil {
				co = m.ParseCodeowners(codeownersText(co) + "/sales/ @acme/sales\n")
			}
			equals(t, tc.want, findingsOf(&m.Input{
				Sources:   []m.Source{{Entities: []m.Entity{unplaced, routed, e}}},
				Artifacts: m.Artifacts{Codeowners: co},
			}, "1.2-C"))
		})
	}
}

// codeownersText writes co back as CODEOWNERS lines.
func codeownersText(co *m.Codeowners) string {
	var b strings.Builder
	for _, r := range co.Rules {
		b.WriteString(strings.Join(append([]string{r.Pattern}, r.Owners...), " ") + "\n")
	}
	return b.String()
}
