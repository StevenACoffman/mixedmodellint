package mixedmodellint_test

import (
	"testing"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// contactTable returns a table repeating the party-contact cluster, its
// fields prefixed by its own name as vendors.vendor_email is.
func contactTable(name, prefix string, kv ...string) m.Entity {
	return entityOf(name, kv, col(name+"_id", m.TypeInt), col(prefix+"name", m.TypeString),
		col(prefix+"email", m.TypeString), col(prefix+"phone", m.TypeString),
		col("street", m.TypeString), col("city", m.TypeString))
}

func TestRepeatedClusters(t *testing.T) {
	t.Parallel()
	party := table("parties", []string{"party_id"}, col("party_id", m.TypeInt))
	cases := map[string]struct {
		entities []m.Entity
		want     []string
	}{
		"three entities repeat contact fields": {
			entities: []m.Entity{
				contactTable("customers", ""), contactTable("employees", ""),
				contactTable("vendor", "vendor_"),
			},
			want: []string{"4.11-A customers"},
		},
		"two is common and fine": {
			entities: []m.Entity{contactTable("customers", ""), contactTable("employees", "")},
		},
		"too few slots filled": {
			entities: []m.Entity{
				table("customers", nil, col("name", m.TypeString), col("email", m.TypeString)),
				table("employees", nil, col("name", m.TypeString), col("email", m.TypeString)),
				table("vendors", nil, col("name", m.TypeString), col("email", m.TypeString)),
			},
		},
		"one referencing a party leaves two": {
			entities: []m.Entity{
				party, contactTable("customers", ""), contactTable("employees", ""),
				referencing(new(contactTable("vendors", "")), "party_id", "parties"),
			},
		},
		"a recorded decision on one member": {
			entities: []m.Entity{
				contactTable("customers", ""), contactTable("employees", ""),
				contactTable("vendors", "", "pattern",
					`party_role=rejected reason="stakeholders say customer, not party"`),
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.want, findingsOf(src(tc.entities...), "4.11-A"))
		})
	}
}

func TestPatternRecords(t *testing.T) {
	t.Parallel()
	rejected := `party_role=rejected reason="self-joins on the hot path" members=3`
	group := func(kv ...string) []m.Entity {
		return []m.Entity{
			contactTable("customers", "", kv...),
			contactTable("employees", "", kv...), contactTable("vendors", "", kv...),
		}
	}
	cases := map[string]struct {
		entities []m.Entity
		want     []string
	}{
		"a group with valid rejections": {entities: group("pattern", rejected)},
		"a group with no records": {
			entities: group(),
			want:     []string{"4.11-B customers", "4.11-B employees", "4.11-B vendors"},
		},
		"a rejection with no reason": {
			entities: group("pattern", "party_role=rejected"),
			want:     []string{"4.11-B customers", "4.11-B employees", "4.11-B vendors"},
		},
		"an unknown pattern": {
			entities: group("pattern", `crm_party=rejected reason="x"`),
			want:     []string{"4.11-B customers", "4.11-B employees", "4.11-B vendors"},
		},
		"an adoption that references no party": {
			entities: group("pattern", "party_role=adopted"),
			want:     []string{"4.11-B customers", "4.11-B employees", "4.11-B vendors"},
		},
		"a rejection weighed for fewer members": {
			entities: append(group("pattern", rejected),
				contactTable("contractors", "", "pattern", rejected)),
			want: []string{
				"4.11-B contractors", "4.11-B customers", "4.11-B employees", "4.11-B vendors",
			},
		},
		"none applicable, with a reason": {
			entities: group(
				"pattern",
				`none-applicable reason="each is a separate bounded context"`,
			),
		},
		"an ADR naming the pattern": {
			entities: group("pattern_adr", "0009-parties"),
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := src(tc.entities...)
			in.Artifacts.ADRs = []m.Document{{
				Path: "docs/adr/0009-parties.md", Text: "We weighed party_role and kept tables.",
			}}
			equals(t, tc.want, findingsOf(in, "4.11-B"))
		})
	}
}

func TestPatternRecordsForNewEntities(t *testing.T) {
	t.Parallel()
	orders := table("orders", []string{"order_id"}, col("order_id", m.TypeInt),
		col("total", m.TypeDecimal), col("placed_at", m.TypeTimestamptz))
	carts := table("carts", []string{"cart_id"}, col("cart_id", m.TypeInt))
	reworked := table("orders", []string{"order_id"}, col("order_id", m.TypeInt),
		col("amount", m.TypeDecimal), col("placed_at", m.TypeTimestamptz))
	cases := map[string]struct {
		current []m.Entity
		locked  []m.Entity
		noLock  bool
		want    []string
	}{
		"a new entity": {
			current: []m.Entity{orders, carts},
			locked:  []m.Entity{orders},
			want:    []string{"4.11-B carts"},
		},
		"an unchanged entity": {current: []m.Entity{orders}, locked: []m.Entity{orders}},
		"a redesigned entity": {
			current: []m.Entity{reworked},
			locked:  []m.Entity{orders},
			want:    []string{"4.11-B orders"},
		},
		"no lock, nothing is new": {current: []m.Entity{orders, carts}, noLock: true},
		"a new entity with a decision": {
			current: []m.Entity{orders, func() m.Entity {
				e := carts
				e.Annotations = ann("pattern", `none-applicable reason="a session scratchpad"`)
				return e
			}()},
			locked: []m.Entity{orders},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := src(tc.current...)
			if !tc.noLock {
				in.Lock = lockOf(t, tc.locked...)
			}
			equals(t, tc.want, findingsOf(in, "4.11-B"))
		})
	}
}
