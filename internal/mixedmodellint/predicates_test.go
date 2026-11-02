package mixedmodellint_test

import (
	"bytes"
	"strings"
	"testing"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// The tables in this file pin each alternative inside the designs'
// predicates (each word a lexicon accepts, each way a key or a history can be
// shaped), one case per alternative, so a change to any of them fails a
// test.

func TestStatePredicates(t *testing.T) {
	t.Parallel()
	tickets := func(f m.Field, checks ...m.Check) m.Entity {
		e := entityOf("tickets", []string{"history", "required"}, col("ticket_id", m.TypeInt), f)
		e.PrimaryKey = []string{"ticket_id"}
		e.Checks = checks
		return e
	}
	update := func(target, column string) m.Query {
		return m.Query{
			Kind: m.QueryUpdate, Target: target, Pos: at(2),
			Assignments: []m.Assignment{{Column: column}},
		}
	}
	insert := m.Query{
		Kind: m.QueryInsert, Target: "tickets", Pos: at(2),
		Assignments: []m.Assignment{{Column: "priority"}},
	}
	three := m.Check{Field: "priority", Enum: []string{"low", "mid", "high"}}
	other := entityOf("queues", nil, col("queue_id", m.TypeInt), col("priority", m.TypeString))
	cases := map[string]struct {
		entities []m.Entity
		queries  []m.Query
		want     []string
	}{
		"enum from a CHECK, updated": {
			entities: []m.Entity{tickets(col("priority", m.TypeString), three)},
			queries:  []m.Query{update("tickets", "priority")},
			want:     []string{"3.1-A tickets priority"},
		},
		"two values are not a workflow": {
			entities: []m.Entity{
				tickets(
					col("priority", m.TypeString),
					m.Check{Field: "priority", Enum: []string{"lo", "hi"}},
				),
			},
			queries: []m.Query{update("tickets", "priority")},
		},
		"a CHECK on another field": {
			entities: []m.Entity{
				tickets(
					col("priority", m.TypeString),
					m.Check{Field: "queue", Enum: []string{"a", "b", "c"}},
				),
			},
			queries: []m.Query{update("tickets", "priority")},
		},
		"a CHECK without values": {
			entities: []m.Entity{
				tickets(
					col("priority", m.TypeString),
					m.Check{Field: "priority", Expr: "length(priority) > 0"},
				),
			},
			queries: []m.Query{update("tickets", "priority")},
		},
		"inserted, not updated": {
			entities: []m.Entity{tickets(col("priority", m.TypeString), three)},
			queries:  []m.Query{insert},
		},
		"another column updated": {
			entities: []m.Entity{tickets(col("priority", m.TypeString), three)},
			queries:  []m.Query{update("tickets", "queue")},
		},
		"the same column of another entity": {
			entities: []m.Entity{tickets(col("priority", m.TypeString), three), other},
			queries:  []m.Query{update("queues", "priority")},
		},
		"stage, phase and state are states": {
			entities: []m.Entity{
				tickets(col("stage", m.TypeString)),
				entityOf(
					"jobs",
					[]string{"history", "required"},
					col(
						"job_id",
						m.TypeInt,
					),
					col("phase", m.TypeString),
					col("run_state", m.TypeString),
				),
			},
			want: []string{"3.1-A jobs phase", "3.1-A jobs run_state", "3.1-A tickets stage"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := &m.Input{Sources: []m.Source{{Entities: tc.entities, Queries: tc.queries}}}
			equals(t, tc.want, findingsOf(in, "3.1-A"))
		})
	}
}

func TestHistoryForms(t *testing.T) {
	t.Parallel()
	tickets := func(kv ...string) m.Entity {
		e := entityOf(
			"tickets",
			append([]string{"history", "required"}, kv...),
			col("ticket_id", m.TypeInt),
			col("status", m.TypeString),
		)
		e.PrimaryKey = []string{"ticket_id"}
		return e
	}
	child := func(name string, kv []string, ref bool, fields ...m.Field) m.Entity {
		e := entityOf(name, kv, append([]m.Field{col("ticket_id", m.TypeInt)}, fields...)...)
		if ref {
			e.Refs = []m.Ref{{Fields: []string{"ticket_id"}, Target: "tickets"}}
		}
		return e
	}
	full := []m.Field{
		col("actor_id", m.TypeInt),
		col("event_type", m.TypeString),
		col("occurred_at", m.TypeTimestamptz),
	}
	byStatus := func(target, relation, column string) m.Query {
		return m.Query{
			Kind:   m.QueryCreateView,
			Target: target,
			Pos:    at(3),
			Relations: []m.Relation{
				{Name: relation, Alias: "t"},
			},
			GroupBy: []m.ColumnRef{{Relation: "t", Column: column}},
		}
	}
	plain := tickets()
	plain.Annotations = ann()
	cases := map[string]struct {
		entities []m.Entity
		queries  []m.Query
		want     []string
	}{
		"history suffix": {
			entities: []m.Entity{tickets(), child("ticket_history", nil, true, full...)},
		},
		"log suffix": {
			entities: []m.Entity{tickets(), child("ticket_log", nil, true, full...)},
		},
		"transitions suffix": {
			entities: []m.Entity{tickets(), child("ticket_transitions", nil, true, full...)},
		},
		"audit suffix": {
			entities: []m.Entity{tickets(), child("ticket_audit", nil, true, full...)},
		},
		"named but unlinked": {
			entities: []m.Entity{tickets(), child("ticket_events", nil, false, full...)},
			want:     []string{"3.1-A tickets status"},
		},
		"history_of another entity": {
			entities: []m.Entity{
				tickets(),
				entityOf("queues", nil, col("queue_id", m.TypeInt)),
				child("trail", []string{"history_of", "queues"}, true, full...),
			},
			want: []string{"3.1-A tickets status"},
		},
		"validity columns": {
			entities: []m.Entity{
				tickets(),
				child(
					"ticket_versions",
					nil,
					true,
					col("valid_from", m.TypeTimestamptz),
					col("valid_to", m.TypeTimestamptz),
				),
			},
		},
		"transaction-time columns": {
			entities: []m.Entity{
				tickets(),
				child(
					"ticket_versions",
					nil,
					true,
					col("txn_from", m.TypeTimestamptz),
					col("txn_to", m.TypeTimestamptz),
				),
			},
		},
		"only one validity column": {
			entities: []m.Entity{
				tickets(),
				child("ticket_versions", nil, true, col("valid_from", m.TypeTimestamptz)),
			},
			want: []string{"3.1-A tickets status"},
		},
		"only one transaction time": {
			entities: []m.Entity{
				tickets(),
				child(
					"ticket_versions",
					nil,
					true,
					col("txn_from", m.TypeTimestamptz),
					col("valid_to", m.TypeTimestamptz),
				),
			},
			want: []string{"3.1-A tickets status"},
		},
		"history_in naming nothing": {
			entities: []m.Entity{tickets("history_in", "nowhere")},
			want:     []string{"3.1-A tickets status"},
		},
		"user as the actor": {entities: []m.Entity{
			tickets(),
			child(
				"ticket_events",
				nil,
				true,
				col(
					"user_id",
					m.TypeInt,
				),
				col("action", m.TypeString),
				col("occurred_at", m.TypeTimestamptz),
			),
		}},
		"agent as the actor": {entities: []m.Entity{
			tickets(),
			child(
				"ticket_events",
				nil,
				true,
				col(
					"agent_id",
					m.TypeInt,
				),
				col("new_state", m.TypeString),
				col("occurred_at", m.TypeTimestamptz),
			),
		}},
		"a _by field as the actor": {
			entities: []m.Entity{
				tickets(),
				child(
					"ticket_events",
					nil,
					true,
					col(
						"changed_by",
						m.TypeString,
					),
					col("to_status", m.TypeString),
					col("occurred_at", m.TypeTimestamptz),
				),
			},
		},
		"a by-word inside a name is not an actor": {
			entities: []m.Entity{
				tickets(),
				child(
					"ticket_events",
					nil,
					true,
					col(
						"by_hand",
						m.TypeString,
					),
					col("event_type", m.TypeString),
					col("occurred_at", m.TypeTimestamptz),
				),
			},
			want: []string{"3.1-A tickets status"},
		},
		"a time stored as text is not when": {
			entities: []m.Entity{
				tickets(),
				child(
					"ticket_events",
					nil,
					true,
					col(
						"actor_id",
						m.TypeInt,
					),
					col("event_type", m.TypeString),
					col("occurred_at", m.TypeString),
				),
			},
			want: []string{"3.1-A tickets status"},
		},
		"a complete history after a thin one": {
			entities: []m.Entity{
				tickets(),
				child("ticket_log", nil, true, col("event_type", m.TypeString)),
				child("ticket_events", nil, true, full...),
			},
		},
		"a thin history after a complete one": {
			entities: []m.Entity{
				tickets(),
				child("ticket_events", nil, true, full...),
				child("ticket_log", nil, true, col("event_type", m.TypeString)),
			},
		},
		"a view on another column": {
			entities: []m.Entity{plain},
			queries:  []m.Query{byStatus("v", "tickets", "ticket_id")},
		},
		"a view on another entity": {
			entities: []m.Entity{plain, entityOf("queues", nil, col("status", m.TypeString))},
			queries:  []m.Query{byStatus("v", "queues", "status")},
			want:     []string{"3.1-A queues status"},
		},
		"a query that is not a view": {entities: []m.Entity{plain}, queries: []m.Query{
			{
				Kind: m.QuerySelect,
				Pos:  at(3),
				Relations: []m.Relation{
					{Name: "tickets", Alias: "t"},
				},
				GroupBy: []m.ColumnRef{{Relation: "t", Column: "status"}},
			},
		}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := &m.Input{Sources: []m.Source{{Entities: tc.entities, Queries: tc.queries}}}
			equals(t, tc.want, findingsOf(in, "3.1-A"))
		})
	}
}

func TestKeyPredicates(t *testing.T) {
	t.Parallel()
	keyed := table
	view := keyed("a_view", nil)
	view.Kind = m.KindView
	byRef := keyed(
		"runs",
		[]string{"pipeline", "started_at"},
		col("pipeline", m.TypeInt),
		col("started_at", m.TypeTimestamptz),
	)
	byRef.Refs = []m.Ref{{Fields: []string{"pipeline"}, Target: "pipelines"}}
	cases := map[string]struct {
		entities []m.Entity
		want     []string
	}{
		"a view, then a keyless table": {
			entities: []m.Entity{view, keyed("runs", nil)},
			want:     []string{"4.1-A runs"},
		},
		"time beside a key column": {
			entities: []m.Entity{
				keyed(
					"runs",
					[]string{"pipeline_key", "started_at"},
					col("pipeline_key", m.TypeInt),
					col("started_at", m.TypeTimestamptz),
				),
			},
		},
		"time beside a reference": {entities: []m.Entity{byRef}},
		"a time-named text key": {
			entities: []m.Entity{
				keyed("runs", []string{"created_at"}, col("created_at", m.TypeString)),
			},
			want: []string{"4.1-A runs created_at"},
		},
		"a key column with no field": {
			entities: []m.Entity{keyed("runs", []string{"ghost"}, col("note", m.TypeString))},
		},
		"a key of the bare word id": {
			entities: []m.Entity{
				keyed("runs", []string{"id", "day"}, col("id", m.TypeInt), col("day", m.TypeDate)),
			},
		},
		"a mutable key, then a stable one": {
			entities: []m.Entity{
				keyed(
					"runs",
					[]string{"handle", "run_id"},
					col("handle", m.TypeString),
					col("run_id", m.TypeInt),
				),
			},
			want: []string{"4.1-A runs handle"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := &m.Input{Sources: []m.Source{{Entities: tc.entities}}}
			equals(t, tc.want, findingsOf(in, "4.1-A"))
		})
	}
}

func TestResolutionPredicates(t *testing.T) {
	t.Parallel()
	plainCustomers := entityOf("customers", nil, col("customer_id", m.TypeInt))
	cases := map[string]struct {
		entities []m.Entity
		want     []string
	}{
		"one declared source": {
			entities: []m.Entity{
				entityOf("customers", []string{"sources", "crm"}, col("customer_id", m.TypeInt)),
			},
		},
		"an unrelated entity first, then a multi-source one": {
			entities: []m.Entity{
				plainCustomers,
				entityOf(
					"accounts",
					nil,
					col("account_id", m.TypeInt),
					col("crm_id", m.TypeString),
					col("erp_key", m.TypeString),
				),
			},
			want: []string{"4.2-A accounts"},
		},
		"prefixed source IDs": {
			entities: []m.Entity{
				entityOf(
					"accounts",
					nil,
					col("account_id", m.TypeInt),
					col("legacy_crm_id", m.TypeString),
					col("src_erp_key", m.TypeString),
				),
			},
			want: []string{"4.2-A accounts"},
		},
		"a declared identity map is the resolution": {
			entities: []m.Entity{
				entityOf(
					"account_keys",
					[]string{"role", "identity_map"},
					col("account_id", m.TypeInt),
					col("source_system", m.TypeString),
				),
			},
		},
		"the entity a resolution names is the mapping": {
			entities: []m.Entity{
				entityOf(
					"accounts",
					[]string{"resolution", "account_map", "resolution_owner", "team:ops"},
					col("account_id", m.TypeInt),
					col("crm_id", m.TypeString),
					col("erp_id", m.TypeString),
				),
				entityOf(
					"account_map",
					nil,
					col("account_id", m.TypeInt),
					col("source_system", m.TypeString),
					col("external_id", m.TypeString),
				),
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := &m.Input{Sources: []m.Source{{Entities: tc.entities}}}
			equals(t, tc.want, findingsOf(in, "4.2-A"))
		})
	}
}

func TestRenamePredicates(t *testing.T) {
	t.Parallel()
	before := table(
		"customers",
		[]string{"customer_id", "region_id"},
		col("customer_id", m.TypeInt),
		col("region_id", m.TypeInt),
	)
	twoRenames := table(
		"customers",
		[]string{"cust_key", "reg_key"},
		col("cust_key", m.TypeInt),
		col("reg_key", m.TypeInt),
	)
	selfMapped := table(
		"customers",
		[]string{"cust_key", "region_id"},
		col("cust_key", m.TypeInt),
		col("region_id", m.TypeInt),
		col("source_system", m.TypeString),
	)
	roleOnly := table("keys", nil, col("other", m.TypeInt))
	roleOnly.Annotations = ann("role", "identity_map")
	renamed := table(
		"customers",
		[]string{"cust_key", "region_id"},
		col("cust_key", m.TypeInt),
		col("region_id", m.TypeInt),
	)
	cases := map[string]struct {
		current []m.Entity
		queries []m.Query
		want    []string
	}{
		"two keys renamed": {
			current: []m.Entity{twoRenames},
			want:    []string{"4.3-A customers cust_key", "4.3-A customers reg_key"},
		},
		"its own columns are no map": {
			current: []m.Entity{selfMapped},
			want:    []string{"4.3-A customers cust_key"},
		},
		"a declared map without the new column": {
			current: []m.Entity{renamed, roleOnly},
			want:    []string{"4.3-A customers cust_key"},
		},
		"an update of an unknown table": {current: []m.Entity{before}, queries: []m.Query{{
			Kind: m.QueryUpdate, Target: "elsewhere", Pos: at(2),
			Assignments: []m.Assignment{{Column: "customer_id"}},
		}}},
		"an insert setting the key": {current: []m.Entity{before}, queries: []m.Query{{
			Kind: m.QueryInsert, Target: "customers", Pos: at(2),
			Assignments: []m.Assignment{{Column: "customer_id"}},
		}}},
		"an update after another statement": {current: []m.Entity{before}, queries: []m.Query{
			{Kind: m.QuerySelect, Pos: at(1)},
			{
				Kind:        m.QueryUpdate,
				Target:      "customers",
				Pos:         at(2),
				Assignments: []m.Assignment{{Column: "customer_id"}},
			},
		}, want: []string{"4.3-A customers customer_id"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := &m.Input{
				Sources: []m.Source{{Entities: tc.current, Queries: tc.queries}},
				Lock:    lockOf(t, before),
			}
			equals(t, tc.want, findingsOf(in, "4.3-A"))
		})
	}
}

func TestDerivedPredicates(t *testing.T) {
	t.Parallel()
	videos := entityOf("videos", []string{"role", "file"}, col("video_id", m.TypeInt))
	videos.PrimaryKey = []string{"video_id"}
	clip := func(refFields []string, target string, keys [][]string, fields ...m.Field) m.Entity {
		e := entityOf("clips", []string{"derived_from", "videos"}, fields...)
		e.Refs = []m.Ref{{Fields: refFields, Target: target}}
		e.Uniques = keys
		return e
	}
	cases := map[string]struct {
		e    m.Entity
		want []string
	}{
		"source by a reference not named after it": {
			e: clip(
				[]string{"vid"},
				"videos",
				[][]string{{"vid", "page"}},
				col("vid", m.TypeInt),
				col("page", m.TypeInt),
			),
		},
		"a two-column reference is not the source id": {
			e: clip(
				[]string{"vid", "part"},
				"videos",
				[][]string{{"vid", "page"}},
				col("vid", m.TypeInt),
				col("part", m.TypeInt),
				col("page", m.TypeInt),
			),
			want: []string{"4.6-A clips"},
		},
		"a reference to something else": {
			e: clip(
				[]string{"vid"},
				"tags",
				[][]string{{"vid", "page"}},
				col("vid", m.TypeInt),
				col("page", m.TypeInt),
			),
			want: []string{"4.6-A clips"},
		},
		"a key over only part of the location": {
			e: clip(
				[]string{"video_id"},
				"videos",
				[][]string{{"video_id", "ts_start"}},
				col("video_id", m.TypeInt),
				col("ts_start", m.TypeInt),
				col("ts_end", m.TypeInt),
			),
			want: []string{"4.6-A clips"},
		},
		"derived from an unknown entity": {
			e:    entityOf("clips", []string{"derived_from", "nothing"}, col("clip_id", m.TypeInt)),
			want: []string{"4.6-A clips"},
		},
		"a file pointer by itself is one signal": {
			e: entityOf(
				"thumbs",
				nil,
				col("thumb_id", m.TypeInt),
				col("image_path", m.TypeString),
			),
			want: []string{"4.6-A thumbs"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := &m.Input{Sources: []m.Source{{Entities: []m.Entity{videos, tc.e}}}}
			equals(t, tc.want, findingsOf(in, "4.6-A"))
		})
	}
}

func TestReferencePredicates(t *testing.T) {
	t.Parallel()
	posts := func(f m.Field, refs ...m.Ref) m.Entity {
		e := table("posts", []string{"post_id"}, col("post_id", m.TypeInt), f)
		e.Refs = refs
		return e
	}
	legacy := posts(col("tag_ids", m.TypeArray))
	legacy.Annotations = ann("source", "legacy")
	cases := map[string]struct {
		entities []m.Entity
		design   string
		want     []string
	}{
		"a single-word ids array": {
			entities: []m.Entity{posts(col("ids", m.TypeArray))},
			design:   "6.1-A",
		},
		"a singular id array": {
			entities: []m.Entity{posts(col("tag_id", m.TypeArray))},
			design:   "6.1-A",
			want:     []string{"6.1-A posts tag_id"},
		},
		"an array referencing nothing": {
			entities: []m.Entity{
				posts(col("tags", m.TypeArray), m.Ref{Fields: []string{"tags"}, Target: "tags"}),
			},
			design: "6.1-A",
		},
		"a legacy table first": {
			entities: []m.Entity{legacy, table("notes", nil, col("label_ids", m.TypeArray))},
			design:   "6.1-A",
			want:     []string{"6.1-A notes label_ids"},
		},
		"a bare _id column": {
			entities: []m.Entity{posts(col("_id", m.TypeInt))},
			design:   "6.2-A",
		},
		"a declared self-reference": {
			entities: []m.Entity{
				posts(
					col("parent_id", m.TypeInt),
					m.Ref{Fields: []string{"parent_id"}, Target: "posts"},
				),
			},
			design: "6.2-A",
		},
		"a legacy table, then an owned one": {
			entities: []m.Entity{
				legacy,
				table("tags", []string{"tag_id"}, col("tag_id", m.TypeInt)),
				table("notes", nil, col("tag_id", m.TypeInt)),
			},
			design: "6.2-A",
			want:   []string{"6.2-A notes tag_id"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := &m.Input{Sources: []m.Source{{Entities: tc.entities}}}
			equals(t, tc.want, findingsOf(in, tc.design))
		})
	}
}

func TestCardinalityPredicates(t *testing.T) {
	t.Parallel()
	lines := func(card string, inferredFirst bool) m.Entity {
		e := table(
			"lines",
			[]string{"line_id"},
			col("line_id", m.TypeInt),
			col("order_id", m.TypeInt),
			fieldWith("shop_id", m.TypeInt, "card", card),
		)
		e.Fields[2].Nullable = true
		e.Refs = []m.Ref{{Fields: []string{"order_id", "shop_id"}, Target: "orders"}}
		if inferredFirst {
			e.Refs = append(
				[]m.Ref{{Fields: []string{"x_id"}, Target: "x", Inferred: true}},
				e.Refs...)
		}
		return e
	}
	cases := map[string]struct {
		e    m.Entity
		want []string
	}{
		"second column nullable, mandatory": {
			e:    lines("2..N", false),
			want: []string{"6.2-B lines shop_id"},
		},
		"a bare zero is optional": {e: lines("0", false)},
		"after an inferred reference": {
			e:    lines("1", true),
			want: []string{"6.2-B lines shop_id"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := &m.Input{Sources: []m.Source{{Entities: []m.Entity{tc.e}}}}
			equals(t, tc.want, findingsOf(in, "6.2-B"))
		})
	}
}

func TestJunctionPredicates(t *testing.T) {
	t.Parallel()
	orders := table(
		"orders",
		[]string{"order_id"},
		col("order_id", m.TypeInt),
		col("note", m.TypeString),
	)
	measured := table(
		"tags",
		[]string{"tag_id"},
		col("tag_id", m.TypeInt),
		col("tag_revenue", m.TypeDecimal),
	)
	plainTags := table("tags", []string{"tag_id"}, col("tag_id", m.TypeInt))
	textPrice := table(
		"tags",
		[]string{"tag_id"},
		col("tag_id", m.TypeInt),
		col("price", m.TypeString),
	)
	link := func(kv []string, pk []string, refs int, extra ...m.Field) m.Entity {
		j := entityOf(
			"order_tags",
			kv,
			append([]m.Field{col("order_id", m.TypeInt), col("tag_id", m.TypeInt)}, extra...)...)
		j.PrimaryKey = pk
		j.Refs = []m.Ref{{Fields: []string{"order_id"}, Target: "orders"}, {Fields: []string{"tag_id"}, Target: "tags"}}[:refs]
		return j
	}
	cases := map[string]struct {
		tags, j m.Entity
		want    []string
	}{
		"the measure on the second side": {
			tags: measured,
			j:    link(nil, []string{"order_id", "tag_id"}, 2),
			want: []string{"8.2-A order_tags"},
		},
		"a number-named text column": {
			tags: textPrice,
			j:    link(nil, []string{"order_id", "tag_id"}, 2),
		},
		"one reference is not a junction": {
			tags: measured,
			j:    link(nil, []string{"id"}, 1, col("id", m.TypeInt)),
		},
		"keyed by both, many other columns": {
			tags: measured,
			j: link(
				nil,
				[]string{"order_id", "tag_id"},
				2,
				col("a", m.TypeString),
				col("b", m.TypeString),
				col("c", m.TypeString),
			),
			want: []string{"8.2-A order_tags"},
		},
		"two other columns beside a key": {
			tags: measured,
			j: link(
				nil,
				[]string{"id"},
				2,
				col("id", m.TypeInt),
				col("a", m.TypeString),
				col("b", m.TypeString),
			),
			want: []string{"8.2-A order_tags"},
		},
		"one other column beside a key": {
			tags: measured,
			j:    link(nil, []string{"id"}, 2, col("id", m.TypeInt), col("a", m.TypeString)),
			want: []string{"8.2-A order_tags"},
		},
		"a policy naming no column": {
			tags: plainTags,
			j:    link([]string{"m2m", "overlapping"}, []string{"order_id", "tag_id"}, 2),
		},
		"a policy opening with its column": {
			tags: plainTags,
			j:    link([]string{"m2m", "(weight)"}, []string{"order_id", "tag_id"}, 2),
			want: []string{"8.2-A order_tags"},
		},
		"a malformed policy": {
			tags: plainTags,
			j:    link([]string{"m2m", "allocated)weight("}, []string{"order_id", "tag_id"}, 2),
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := &m.Input{Sources: []m.Source{{Entities: []m.Entity{orders, tc.tags, tc.j}}}}
			equals(t, tc.want, findingsOf(in, "8.2-A"))
		})
	}
}

func TestMeasurePredicates(t *testing.T) {
	t.Parallel()
	facts := func(pk []string, fields ...m.Field) m.Entity {
		e := entityOf("sales", []string{"layer", "analytical"}, fields...)
		e.PrimaryKey = pk
		return e
	}
	coded := facts(nil, col("tier", m.TypeInt))
	coded.Checks = []m.Check{{Field: "tier", Enum: []string{"1", "2"}}}
	cases := map[string]struct {
		entities []m.Entity
		want     []string
	}{
		"a numeric key column": {
			entities: []m.Entity{facts([]string{"line_no"}, col("line_no", m.TypeInt))},
		},
		"a code constrained by a CHECK": {entities: []m.Entity{coded}},
		"balance declared additive": {
			entities: []m.Entity{
				facts(nil, fieldWith("balance", m.TypeDecimal, "measure", "additive")),
			},
			want: []string{"8.3-A sales balance"},
		},
		"on hand declared additive": {
			entities: []m.Entity{
				facts(nil, fieldWith("on_hand", m.TypeInt, "measure", "additive")),
			},
			want: []string{"8.3-A sales on_hand"},
		},
		"an oltp table, then analytical one": {
			entities: []m.Entity{
				entityOf("carts", []string{"layer", "oltp"}, col("total", m.TypeInt)),
				facts(nil, col("revenue", m.TypeDecimal)),
			},
			want: []string{"8.3-A sales revenue"},
		},
		"each analytical layer": {entities: []m.Entity{
			entityOf("f1", []string{"layer", "feature"}, col("units", m.TypeInt)),
			entityOf("f2", []string{"layer", "obt"}, col("units", m.TypeInt)),
			entityOf("f3", []string{"layer", "event"}, col("units", m.TypeInt)),
		}, want: []string{"8.3-A f1 units", "8.3-A f2 units", "8.3-A f3 units"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := &m.Input{Sources: []m.Source{{Entities: tc.entities}}}
			equals(t, tc.want, findingsOf(in, "8.3-A"))
		})
	}
}

func TestTimePredicates(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		e      m.Entity
		design string
		want   []string
	}{
		"staging data without ingestion": {
			e: entityOf(
				"clicks",
				[]string{"layer", "staging"},
				col("clicked_at", m.TypeTimestamptz),
			),
			design: "9.1-A",
			want:   []string{"9.1-A clicks"},
		},
		"landed data with no times": {
			e:      entityOf("clicks", []string{"layer", "event"}, col("click_id", m.TypeInt)),
			design: "9.1-A",
		},
		"two times, one ambiguous": {
			e: entityOf(
				"visits",
				[]string{"layer", "core"},
				col("created_at", m.TypeTimestamptz),
				col("visit_date", m.TypeDate),
			),
			design: "9.1-A",
		},
		"a declared system time": {
			e: entityOf(
				"visits",
				[]string{"layer", "core"},
				fieldWith("seen_at", m.TypeTimestamptz, "time", "system"),
			),
			design: "9.1-A",
			want:   []string{"9.1-A visits"},
		},
		"a declared ingestion time": {
			e: entityOf(
				"visits",
				[]string{"layer", "core"},
				fieldWith("seen_at", m.TypeTimestamptz, "time", "ingested"),
			),
			design: "9.1-A",
			want:   []string{"9.1-A visits"},
		},
		"an unknown time class": {
			e: entityOf(
				"visits",
				[]string{"layer", "core"},
				fieldWith("seen_at", m.TypeTimestamptz, "time", "whenever"),
			),
			design: "9.1-A",
		},
		"a validity bound as text": {
			e:      table("prices", nil, col("valid_from", m.TypeString)),
			design: "9.2-A",
			want:   []string{"9.2-A prices valid_from"},
		},
		"an end bound as text": {
			e:      table("prices", nil, col("valid_to", m.TypeString)),
			design: "9.2-A",
			want:   []string{"9.2-A prices valid_to"},
		},
		"an encoded integer time": {
			e:      table("prices", nil, fieldWith("seen_at", m.TypeInt, "encoding", "unix_ms")),
			design: "9.2-A",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := &m.Input{Sources: []m.Source{{Entities: []m.Entity{tc.e}}}}
			equals(t, tc.want, findingsOf(in, tc.design))
		})
	}
}

func TestFilePredicates(t *testing.T) {
	t.Parallel()
	uri, fname, hash := col(
		"object_uri",
		m.TypeString,
	), col(
		"filename",
		m.TypeString,
	), col(
		"content_sha256",
		m.TypeString,
	)
	photo := func(kv []string, keys [][]string, fields ...m.Field) m.Entity {
		e := entityOf("photos", kv, fields...)
		if len(keys) > 0 {
			e.PrimaryKey, e.Uniques = keys[0], keys[1:]
		}
		return e
	}
	cases := map[string]struct {
		entities []m.Entity
		design   string
		want     []string
	}{
		"a creation-named time declared ingestion": {
			entities: []m.Entity{
				photo(
					nil,
					[][]string{{"object_uri"}},
					uri,
					fname,
					hash,
					fieldWith(
						"captured_at",
						m.TypeTimestamptz,
						"time",
						"ingested",
					),
				),
			},
			design: "9.4-A",
			want:   []string{"9.4-A photos"},
		},
		"dedup on one time only": {
			entities: []m.Entity{
				photo(
					[]string{"dedup", "captured_at"},
					[][]string{{"object_uri"}},
					uri,
					fname,
					col(
						"captured_at",
						m.TypeTimestamptz,
					),
					col("ingested_at", m.TypeTimestamptz),
				),
			},
			design: "9.4-A",
			want:   []string{"9.4-A photos"},
		},
		"dedup on the ingestion time only": {
			entities: []m.Entity{
				photo(
					[]string{"dedup", "ingested_at"},
					[][]string{{"object_uri"}},
					uri,
					fname,
					col(
						"captured_at",
						m.TypeTimestamptz,
					),
					col("ingested_at", m.TypeTimestamptz),
				),
			},
			design: "9.4-A",
			want:   []string{"9.4-A photos"},
		},
		"dedup without a creation time": {
			entities: []m.Entity{
				photo(
					[]string{"dedup", "captured_at, ingested_at"},
					[][]string{{"object_uri"}},
					uri,
					fname,
					col(
						"ingested_at",
						m.TypeTimestamptz,
					),
				),
			},
			design: "9.4-A",
			want:   []string{"9.4-A photos"},
		},
		"a key of name and scope": {
			entities: []m.Entity{
				photo(nil, [][]string{{"owner_id", "filename"}}, uri, fname, hash),
			},
			design: "9.4-B",
		},
		"a unique on the name only": {
			entities: []m.Entity{
				photo(nil, [][]string{{"object_uri"}, {"filename"}}, uri, fname, hash),
			},
			design: "9.4-B",
			want:   []string{"9.4-B photos"},
		},
		"a plain table first": {
			entities: []m.Entity{
				table("notes", nil, col("n", m.TypeString)),
				photo(nil, [][]string{{"filename"}}, uri, fname),
			},
			design: "9.4-B",
			want:   []string{"9.4-B photos"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := &m.Input{Sources: []m.Source{{Entities: tc.entities}}}
			equals(t, tc.want, findingsOf(in, tc.design))
		})
	}
}

func TestLockPredicates(t *testing.T) {
	t.Parallel()
	orders := table("orders", []string{"order_id"}, col("order_id", m.TypeInt))
	lines := table(
		"lines",
		[]string{"line_id"},
		col("line_id", m.TypeInt),
		col("order_id", m.TypeInt),
	)
	lines.Refs = []m.Ref{{Fields: []string{"order_id"}, Target: "orders"}}
	retargeted := lines
	retargeted.Refs = []m.Ref{{Fields: []string{"order_id"}, Target: "carts"}}
	unique := table("orders", []string{"order_id"}, col("order_id", m.TypeInt))
	unique.Uniques = [][]string{{"order_id"}}

	var buf bytes.Buffer
	ok(
		t,
		m.WriteLock(&buf, m.NewLock(&m.Input{Sources: []m.Source{{Entities: []m.Entity{orders}}}})),
	)
	if !strings.Contains(buf.String(), `"version": 1,`) {
		t.Fatalf("lock is not version 1:\n%s", buf.String())
	}
	err := m.WriteLock(failingWriter{}, m.Lock{})
	equals(t, "mixedmodellint.WriteLock: disk full", err.Error())

	stale := func(locked, current m.Entity) []string {
		return findingsOf(
			&m.Input{Sources: []m.Source{{Entities: []m.Entity{current}}}, Lock: lockOf(t, locked)},
			"mml/lock-stale",
		)
	}
	equals(t, []string{"mml/lock-stale lines"}, stale(lines, retargeted))
	equals(t, []string(nil), stale(orders, unique)) // a primary key is already unique: same roles
}

func TestRefDeclarationPredicates(t *testing.T) {
	t.Parallel()
	declared := table(
		"lines",
		[]string{"line_id"},
		col("line_id", m.TypeInt),
		fieldWith("order_id", m.TypeInt, "ref", "carts"),
	)
	declared.Refs = []m.Ref{{Fields: []string{"order_id"}, Target: "orders"}}
	r := m.Analyze(&m.Input{Sources: []m.Source{{Entities: []m.Entity{
		table("orders", []string{"order_id"}, col("order_id", m.TypeInt)),
		table("carts", []string{"cart_id"}, col("cart_id", m.TypeInt)),
		declared,
	}}}})
	// The foreign key in the DDL wins over the annotation, so the lock and
	// every design see orders, not carts.
	var buf bytes.Buffer
	ok(
		t,
		m.WriteLock(
			&buf,
			m.NewLock(&m.Input{Sources: []m.Source{{Entities: []m.Entity{declared}}}}),
		),
	)
	if !strings.Contains(buf.String(), `"target": "orders"`) ||
		strings.Contains(buf.String(), `"carts"`) {
		t.Fatalf("declared foreign key replaced:\n%s", buf.String())
	}
	equals(t, 0, countDesign(r, "6.2-A"))
}
