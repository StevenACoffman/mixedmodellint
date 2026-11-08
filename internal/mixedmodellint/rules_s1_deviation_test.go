package mixedmodellint_test

import (
	"testing"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

func TestUndocumentedShortcuts(t *testing.T) {
	t.Parallel()
	catalog := func(doc string, kv ...string) m.Entity {
		e := table("products", []string{"product_id"}, col("product_id", m.TypeInt),
			documented("attributes", m.TypeJSON, doc, kv...))
		return e
	}
	full := m.Document{Path: "docs/adr/0007-catalog-json.md", Text: "# Catalog as JSON\n\n" +
		"## Rationale\n\nLaunch deadline.\n\n## Consequences\n\n" +
		"Every analytics extract must unpack attributes.\n"}
	noCost := m.Document{Path: "docs/adr/0008-no-cost.md", Text: "## Rationale\n\nSpeed.\n\n" +
		"## Cost\n\nTODO\n"}
	cases := map[string]struct {
		entity m.Entity
		adrs   []m.Document
		want   []string
	}{
		"a shortcut with no record": {
			entity: catalog("Attributes as JSON for now."),
			want:   []string{"1.9-B products attributes"},
		},
		"a description with no marker": {
			entity: catalog("Attributes the vendor sends, as received."),
		},
		"a marker inside a word is not one": {
			entity: catalog("Shopping hackathon prizes."),
		},
		"a record naming a full ADR by file name": {
			entity: catalog("TODO: split into columns.", "deviation", "0007-catalog-json"),
			adrs:   []m.Document{full},
		},
		"a record naming a full ADR by path": {
			entity: catalog("Hack until we have a catalog service.", "deviation",
				"docs/adr/0007-catalog-json.md"),
			adrs: []m.Document{full},
		},
		"a record whose ADR leaves the cost TODO": {
			entity: catalog("For now.", "deviation", "0008-no-cost"),
			adrs:   []m.Document{noCost},
			want:   []string{"1.9-B products attributes"},
		},
		"an issue reference is not a record": {
			entity: catalog("Temporary, see JIRA-123.", "deviation", "JIRA-123"),
			adrs:   []m.Document{full},
			want:   []string{"1.9-B products attributes"},
		},
		"the entity's record covers its fields": {
			entity: func() m.Entity {
				e := catalog("FIXME")
				e.Annotations = ann("deviation", "0007-catalog-json")
				return e
			}(),
			adrs: []m.Document{full},
		},
		"the entity's own description": {
			entity: func() m.Entity {
				e := catalog("")
				e.Doc = "Quick fix: one table for both product kinds."
				return e
			}(),
			want: []string{"1.9-B products"},
		},
		"scratch work is temporary by design": {
			entity: func() m.Entity {
				e := catalog("For now.")
				e.Annotations = ann("lifecycle", "scratch")
				return e
			}(),
		},
		"a vendor's comments are not our shortcuts": {
			entity: func() m.Entity {
				e := catalog("TODO")
				e.Annotations = ann("source", "third_party")
				return e
			}(),
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := src(tc.entity)
			in.Artifacts.ADRs = tc.adrs
			equals(t, tc.want, findingsOf(in, "1.9-B"))
		})
	}
}
