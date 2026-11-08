package mixedmodellint_test

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// messageCase is one variant of a design's message: the input that provokes
// it and the designs whose findings the golden file shows.
type messageCase struct {
	in      *m.Input
	designs []string
}

// placed returns e with every field positioned on its own line of file, and
// the entity on line 1, so the golden file shows where each finding points.
func placed(file string, entity *m.Entity) m.Entity {
	e := *entity
	e.Pos = m.Position{File: file, Line: 1}
	e.Fields = slices.Clone(e.Fields)
	for i := range e.Fields {
		e.Fields[i].Pos = m.Position{File: file, Line: i + 2}
	}
	return e
}

func src(entities ...m.Entity) *m.Input {
	return &m.Input{Sources: []m.Source{{Entities: entities}}}
}

func withQueries(in *m.Input, qs ...m.Query) *m.Input {
	in.Sources[0].Queries = append(in.Sources[0].Queries, qs...)
	return in
}

// messageCases provokes each message variant of the designs that compare,
// classify or declare, one input each, so no variant hides another.
func messageCases(t *testing.T) map[string]messageCase {
	t.Helper()
	nullable := func(f m.Field) m.Field { f.Nullable = true; return f }
	orders := placed(
		"orders.sql",
		new(table("orders", []string{"order_id"}, col("order_id", m.TypeInt))),
	)
	customers := placed(
		"customers.sql",
		new(table("customers", []string{"customer_id"}, col("customer_id", m.TypeInt))),
	)
	clicks := placed("clicks.sql", new(entityOf("clicks", []string{"layer", "event"},
		col("click_id", m.TypeInt), col("clicked_at", m.TypeTimestamptz))))
	clicks.PrimaryKey = []string{"click_id"}
	daily := m.Query{
		Kind:   m.QueryCreateView,
		Target: "daily_clicks",
		Pos:    at(2),
		Relations: []m.Relation{
			{Name: "clicks", Alias: "c"},
		},
		Aggregates: []m.Aggregate{{Func: "count", Star: true}},
	}
	lines := func(f m.Field) m.Entity {
		e := placed(
			"lines.sql",
			new(table("lines", []string{"line_id"}, col("line_id", m.TypeInt), f)),
		)
		e.Refs = []m.Ref{{Fields: []string{f.Name}, Target: "orders"}}
		return e
	}
	junction := func(kv []string, x m.Entity) *m.Input {
		j := placed(
			"order_tags.sql",
			new(entityOf("order_tags", kv, col("order_id", m.TypeInt), col("tag_id", m.TypeInt))),
		)
		j.PrimaryKey = []string{"order_id", "tag_id"}
		j.Refs = []m.Ref{
			{Fields: []string{"order_id"}, Target: "orders"},
			{Fields: []string{"tag_id"}, Target: "tags"},
		}
		return src(
			x,
			placed("tags.sql", new(table("tags", []string{"tag_id"}, col("tag_id", m.TypeInt)))),
			j,
		)
	}
	sales := func(f m.Field) *m.Input {
		e := placed(
			"sales.sql",
			new(entityOf("sales", []string{"layer", "analytical"}, col("sale_id", m.TypeInt), f)),
		)
		e.PrimaryKey = []string{"sale_id"}
		return src(e)
	}
	photos := func(kv []string, pk []string, fields ...m.Field) *m.Input {
		e := placed("photos.sql", new(entityOf("photos", kv, fields...)))
		e.PrimaryKey = pk
		return src(e)
	}
	uri, fname := col("object_uri", m.TypeString), col("filename", m.TypeString)
	stateful := func(kv ...string) m.Entity {
		e := placed(
			"tickets.sql",
			new(entityOf("tickets", kv, col("ticket_id", m.TypeInt), col("status", m.TypeString))),
		)
		e.PrimaryKey = []string{"ticket_id"}
		return e
	}
	ticketEvents := func(name string, fields ...m.Field) m.Entity {
		e := placed(
			name+".sql",
			new(entityOf(name, nil, append([]m.Field{col("ticket_id", m.TypeInt)}, fields...)...)),
		)
		e.Refs = []m.Ref{{Fields: []string{"ticket_id"}, Target: "tickets"}}
		return e
	}
	byStatus := m.Query{
		Kind:   m.QueryCreateView,
		Target: "tickets_by_status",
		Pos:    at(3),
		Relations: []m.Relation{
			{Name: "tickets", Alias: "t"},
		},
		GroupBy: []m.ColumnRef{{Relation: "t", Column: "status"}},
	}
	goStruct := placed("model.go", new(m.Entity{
		ID:     "go:m.Visit",
		Kind:   m.KindStruct,
		Source: m.SourceGo,
		Name:   "Visit",
		PrimaryKey: []string{
			"visit_id",
		},
		Fields: []m.Field{col("visit_id", m.TypeInt), col("seen_at", m.TypeString)},
	}))
	jsonSchema := placed("visit.schema.json", new(m.Entity{
		ID:     "json:Visit",
		Kind:   m.KindSchema,
		Source: m.SourceJSONSchema,
		Name:   "Visit",
		PrimaryKey: []string{
			"visit_id",
		},
		Fields: []m.Field{col("visit_id", m.TypeInt), col("seen_at", m.TypeString)},
	}))
	customersMulti := func(kv ...string) m.Entity {
		e := placed("customers.sql", new(entityOf("customers", kv, col("customer_id", m.TypeInt),
			col("crm_id", m.TypeString), col("stripe_id", m.TypeString))))
		e.PrimaryKey = []string{"customer_id"}
		return e
	}
	idMap := placed(
		"customer_map.sql",
		new(table("customer_map", nil, col("customer_id", m.TypeInt),
			m.Field{Name: "source_system", Type: m.TypeString, Enum: []string{"crm"}})),
	)
	videos := placed(
		"videos.sql",
		new(entityOf("videos", []string{"role", "file"}, col("video_id", m.TypeInt))),
	)
	videos.PrimaryKey = []string{"video_id"}
	detections := placed(
		"detections.sql",
		new(entityOf("detections", nil, col("detection_id", m.TypeInt),
			col("video_filename", m.TypeString), col("label", m.TypeString))),
	)
	labels := placed(
		"labels.sql",
		new(entityOf(
			"labels",
			[]string{"derived_from", "videos"},
			col("label_id", m.TypeInt),
			col("video_id", m.TypeInt),
		)),
	)
	labels.Refs = []m.Ref{{Fields: []string{"video_id"}, Target: "videos"}}
	pages := placed(
		"pages.sql",
		new(entityOf(
			"pages",
			[]string{"derived_from", "videos"},
			col("page_id", m.TypeInt),
			col("video_id", m.TypeInt),
			col("page", m.TypeInt),
		)),
	)
	pages.Refs = []m.Ref{{Fields: []string{"video_id"}, Target: "videos"}}
	full := []string{"definition", goodDefinition, "owner", "team:orders", "escalation", "role:cto"}
	gapRecord := m.OwnershipRecord{Owner: "team:orders", Responsibilities: []string{"stuff"}}
	renamedBefore := table("accounts", []string{"account_id"}, col("account_id", m.TypeInt))
	renamedAfter := placed(
		"accounts.sql",
		new(table("accounts", []string{"acct_key"}, col("acct_key", m.TypeInt))),
	)
	holder := placed(
		"holders.sql",
		new(table(
			"holders",
			[]string{"holder_id"},
			col("holder_id", m.TypeInt),
			col("acct_key", m.TypeInt),
		)),
	)
	holder.Refs = []m.Ref{{Fields: []string{"acct_key"}, Target: "accounts"}}
	cart := placed(
		"carts.sql",
		new(
			table(
				"carts",
				[]string{"cart_id"},
				col("cart_id", m.TypeInt),
				col("owner", m.TypeString),
			),
		),
	)
	return map[string]messageCase{
		"1.1-B no record": {
			in:      &m.Input{Sources: []m.Source{{Entities: []m.Entity{cart}}}, Lock: lockOf(t)},
			designs: []string{"1.1-B"},
		},
		"1.1-B disagreeing": {
			in: &m.Input{
				Sources: []m.Source{{Entities: []m.Entity{cart}}},
				Lock:    lockOf(t),
				Artifacts: m.Artifacts{
					Logical: []m.LogicalRecord{
						{
							Entity:     "cart",
							Attributes: []string{"cart_id", "shopper"},
							Keys:       []string{"owner"},
							Pos:        m.Position{File: "model/logical/cart.yaml", Line: 1},
						},
					},
				},
			},
			designs: []string{"1.1-B"},
		},
		"1.2-A nothing": {
			in:      src(placed("orders.sql", new(governedOrders()))),
			designs: []string{"1.2-A"},
		},
		"1.2-A every flaw": {
			in: src(
				placed(
					"orders.sql",
					new(governedOrders(
						"definition",
						"TODO",
						"owner",
						"orders team",
						"escalation",
						"orders team",
					)),
				),
			),
			designs: []string{"1.2-A"},
		},
		"1.2-A same person": {
			in: src(
				placed(
					"orders.sql",
					new(governedOrders(
						"definition",
						goodDefinition,
						"owner",
						"team:x",
						"escalation",
						"team:x",
					)),
				),
			),
			designs: []string{"1.2-A"},
		},
		"1.2-A stale": {
			in: src(
				placed(
					"orders.sql",
					new(governedOrders(
						append(
							slices.Clone(full[2:]),
							"definition",
							goodDefinition+" fp=000000000000",
						)...)),
				),
			),
			designs: []string{"1.2-A"},
		},
		"1.13-A no record": {
			in:      src(placed("orders.sql", new(governedOrders("owner", "team:orders")))),
			designs: []string{"1.13-A"},
		},
		"1.13-A liability": {
			in: &m.Input{
				Sources: []m.Source{
					{
						Entities: []m.Entity{
							placed("orders.sql", new(governedOrders("owner", "team:orders"))),
						},
					},
				},
				Artifacts: m.Artifacts{
					Ownership: []m.OwnershipRecord{{Owner: "team:orders", Liability: true}},
				},
			},
			designs: []string{"1.13-A"},
		},
		"1.13-A gaps": {
			in: &m.Input{
				Sources: []m.Source{
					{
						Entities: []m.Entity{
							placed("orders.sql", new(governedOrders("owner", "team:orders"))),
						},
					},
				},
				Artifacts: m.Artifacts{Ownership: []m.OwnershipRecord{gapRecord}},
			},
			designs: []string{"1.13-A"},
		},
		"3.1-A latest only": {
			in:      withQueries(src(stateful("history", "latest_only")), byStatus),
			designs: []string{"3.1-A"},
		},
		"3.1-A none kept": {in: src(stateful("history", "required")), designs: []string{"3.1-A"}},
		"3.1-A best of two": {
			in: src(
				stateful("history", "required"),
				ticketEvents("ticket_log"),
				ticketEvents("ticket_events", col("event_type", m.TypeString)),
			),
			designs: []string{"3.1-A"},
		},
		"4.1-A no key": {
			in:      src(placed("runs.sql", new(table("runs", nil, col("note", m.TypeString))))),
			designs: []string{"4.1-A"},
		},
		"4.1-A unstable": {
			in: src(
				placed(
					"runs.sql",
					new(table(
						"runs",
						[]string{"email", "started_at"},
						col("email", m.TypeString),
						col("started_at", m.TypeTimestamptz),
					)),
				),
			),
			designs: []string{"4.1-A"},
		},
		"4.2-A undeclared": {in: src(customersMulti()), designs: []string{"4.2-A"}},
		"4.2-A bad mapping": {
			in: src(
				customersMulti("resolution", "customer_map", "resolution_owner", "sales"),
				idMap,
			),
			designs: []string{"4.2-A"},
		},
		"4.2-A no mapping": {
			in: &m.Input{
				Sources: []m.Source{
					{
						Entities: []m.Entity{
							customersMulti(
								"resolution",
								"nowhere",
								"resolution_owner",
								"team:growth",
							),
						},
					},
				},
				Artifacts: m.Artifacts{Ownership: []m.OwnershipRecord{{Owner: "team:sales"}}},
			},
			designs: []string{"4.2-A"},
		},
		"4.3-A renamed": {
			in: &m.Input{
				Sources: []m.Source{{Entities: []m.Entity{renamedAfter, holder}}},
				Lock:    lockOf(t, renamedBefore),
			},
			designs: []string{"4.3-A"},
		},
		"4.3-A rewritten": {
			in: withQueries(
				src(placed("accounts.sql", new(renamedBefore))),
				m.Query{
					Kind:        m.QueryUpdate,
					Target:      "accounts",
					Pos:         at(4),
					Assignments: []m.Assignment{{Column: "account_id"}},
				},
			),
			designs: []string{"4.3-A"},
		},
		"4.6-A one signal": {
			in: src(
				placed(
					"segments.sql",
					new(entityOf("segments", nil, col("segment_id", m.TypeInt))),
				),
			),
			designs: []string{"4.6-A"},
		},
		"4.6-A by filename": {in: src(videos, detections), designs: []string{"4.6-A"}},
		"4.6-A no location": {in: src(videos, labels), designs: []string{"4.6-A"}},
		"4.6-A no key":      {in: src(videos, pages), designs: []string{"4.6-A"}},
		"5.3-A variants": {
			in: src(placed("orders.sql", new(table(
				"orders",
				[]string{"order_id"},
				col("order_id", m.TypeInt),
				col("order_type", m.TypeString),
				nullable(
					col("a", m.TypeString),
				),
				nullable(fieldWith("b", m.TypeString, "null", "absent")),
				nullable(
					fieldWith("c", m.TypeString, "null", "forbidden"),
				),
			)))),
			designs: []string{"5.3-A"},
		},
		"5.3-A not applicable": {
			in: src(placed("orders.sql", new(table(
				"orders",
				[]string{"order_id"},
				col("order_id", m.TypeInt),
				nullable(
					fieldWith("d", m.TypeString, "null", "not_applicable"),
				),
			)))),
			designs: []string{"5.3-A"},
		},
		"6.1-A lists": {
			in: src(
				placed(
					"posts.sql",
					new(table(
						"posts",
						[]string{"post_id"},
						col("post_id", m.TypeInt),
						col("tag_ids", m.TypeArray),
					)),
				),
			),
			designs: []string{"6.1-A"},
		},
		"6.2-A no foreign key": {
			in: src(
				customers,
				placed(
					"orders.sql",
					new(table(
						"orders",
						[]string{"order_id"},
						col("order_id", m.TypeInt),
						col("customer_id", m.TypeInt),
					)),
				),
			),
			designs: []string{"6.2-A"},
		},
		"6.2-B undeclared": {
			in:      src(orders, lines(nullable(col("order_id", m.TypeInt)))),
			designs: []string{"6.2-B"},
		},
		"6.2-B mandatory": {
			in:      src(orders, lines(nullable(fieldWith("order_id", m.TypeInt, "card", "1..N")))),
			designs: []string{"6.2-B"},
		},
		"7.5-B removed": {
			in:      withQueries(&m.Input{Sources: []m.Source{{}}, Lock: lockOf(t, clicks)}, daily),
			designs: []string{"7.5-B"},
		},
		"7.5-B dropped": {
			in: withQueries(
				src(),
				daily,
				m.Query{Kind: m.QueryDrop, Target: "clicks", Pos: at(9)},
			),
			designs: []string{"7.5-B"},
		},
		"7.5-B truncated": {
			in: withQueries(
				src(clicks),
				daily,
				m.Query{Kind: m.QueryTruncate, Target: "clicks", Pos: at(9)},
			),
			designs: []string{"7.5-B"},
		},
		"7.5-B retention": {in: withQueries(src(clicks), daily, m.Query{
			Kind: m.QueryDelete, Target: "clicks", Pos: at(9),
			Filters: []m.ColumnRef{{Relation: "clicks", Column: "clicked_at"}},
		}), designs: []string{"7.5-B"}},
		"8.2-A hazard": {
			in: junction(
				nil,
				placed(
					"orders.sql",
					new(table(
						"orders",
						[]string{"order_id"},
						col("order_id", m.TypeInt),
						col("order_total", m.TypeDecimal),
					)),
				),
			),
			designs: []string{"8.2-A"},
		},
		"8.2-A missing column": {
			in:      junction([]string{"m2m", "primary(is_primary)"}, orders),
			designs: []string{"8.2-A"},
		},
		"8.3-A undeclared": {
			in:      sales(col("stock_level", m.TypeInt)),
			designs: []string{"8.3-A"},
		},
		"8.3-A default guess": {in: sales(col("units", m.TypeInt)), designs: []string{"8.3-A"}},
		"8.3-A not a class": {
			in:      sales(fieldWith("units", m.TypeInt, "measure", "summable")),
			designs: []string{"8.3-A"},
		},
		"8.3-A no over": {
			in:      sales(fieldWith("units", m.TypeInt, "measure", "semi_additive")),
			designs: []string{"8.3-A"},
		},
		"8.3-A contradiction": {
			in:      sales(fieldWith("fee", m.TypeDecimal, "measure", "non_additive")),
			designs: []string{"8.3-A"},
		},
		"9.1-A conflict": {
			in: src(
				placed(
					"loads.sql",
					new(entityOf(
						"loads",
						[]string{"layer", "core"},
						col("loaded_event_at", m.TypeTimestamptz),
						col("ran_at", m.TypeTimestamptz),
					)),
				),
			),
			designs: []string{"9.1-A"},
		},
		"9.1-A system only": {
			in: src(
				placed(
					"loads.sql",
					new(entityOf(
						"loads",
						[]string{"layer", "core"},
						col("loaded_at", m.TypeTimestamptz),
					)),
				),
			),
			designs: []string{"9.1-A"},
		},
		"9.1-A landed": {in: src(clicks), designs: []string{"9.1-A"}},
		"9.2-A per source": {
			in: src(
				goStruct,
				jsonSchema,
				placed(
					"visits.sql",
					new(table(
						"visits",
						[]string{"visit_id"},
						col("visit_id", m.TypeInt),
						col("seen_at", m.TypeString),
						col("left_at", m.TypeInt),
						m.Field{Name: "day_on", Type: m.TypeString, RawType: "varchar(10)"},
					)),
				),
			),
			designs: []string{"9.2-A"},
		},
		"9.4-A no creation": {
			in: photos(
				nil,
				[]string{"content_sha256"},
				uri,
				fname,
				col("ingested_at", m.TypeTimestamptz),
				col("content_sha256", m.TypeString),
			),
			designs: []string{"9.4-A"},
		},
		"9.4-A no ingestion": {
			in: photos(
				nil,
				[]string{"object_uri"},
				uri,
				fname,
				col("captured_at", m.TypeTimestamptz),
			),
			designs: []string{"9.4-A"},
		},
		"9.4-A event times only": {
			in: photos(
				nil,
				[]string{"content_sha256"},
				uri,
				fname,
				col("content_sha256", m.TypeString),
				fieldWith("seen_at", m.TypeTimestamptz, "time", "event"),
				fieldWith("seen2_at", m.TypeTimestamptz, "time", "event"),
			),
			designs: []string{"9.4-A"},
		},
		"9.4-B by name": {
			in:      photos(nil, []string{"filename"}, uri, fname),
			designs: []string{"9.4-B"},
		},
		"9.4-B dedup name": {
			in:      photos([]string{"dedup", "filename"}, []string{"object_uri"}, uri, fname),
			designs: []string{"9.4-B"},
		},
		"9.4-B no key": {in: photos(nil, nil, uri, fname), designs: []string{"9.4-B"}},
	}
}

// TestDesignMessages renders one finding of each message variant, so a
// change to any message, suggestion, position or severity shows in review
// as a diff of testdata/render/designs.txt. Run with -update to rewrite it.
func TestDesignMessages(t *testing.T) {
	t.Parallel()
	cases := messageCases(t)
	for name, tc := range moreMessageCases(t) {
		cases[name] = tc
	}
	for name, tc := range declarationMessageCases(t) {
		cases[name] = tc
	}
	for name, tc := range linkedMessageCases(t) {
		cases[name] = tc
	}
	for name, tc := range queryMessageCases(t) {
		cases[name] = tc
	}
	for name, tc := range s2MessageCases(t) {
		cases[name] = tc
	}
	for name, tc := range s10MessageCases(t) {
		cases[name] = tc
	}
	names := make([]string, 0, len(cases))
	for name := range cases {
		names = append(names, name)
	}
	slices.Sort(names)
	var buf bytes.Buffer
	for _, name := range names {
		tc := cases[name]
		r := m.Analyze(tc.in)
		r.Findings = slices.DeleteFunc(r.Findings, func(f m.Finding) bool {
			return !slices.Contains(tc.designs, f.Design)
		})
		if len(r.Findings) == 0 {
			t.Errorf("%s: no finding of %s", name, strings.Join(tc.designs, ", "))
		}
		buf.WriteString("== " + name + "\n")
		ok(t, m.WriteText(&buf, m.Report{Findings: r.Findings}))
	}
	golden := filepath.Join("testdata", "render", "designs.txt")
	if *update {
		ok(t, os.WriteFile(golden, buf.Bytes(), 0o600))
	}
	want, err := os.ReadFile(golden)
	ok(t, err)
	equals(t, string(want), buf.String())
}

// annAt builds annotations from alternating keys and values, each declared
// at pos.
func annAt(pos m.Position, kv ...string) m.Annotations {
	a := m.Annotations{}
	for i := 0; i+1 < len(kv); i += 2 {
		a[kv[i]] = m.Annotation{Value: kv[i+1], Pos: pos}
	}
	return a
}

// moreMessageCases covers the designs added after messageCases was written,
// and the variants that only differ in a message.
func moreMessageCases(t *testing.T) map[string]messageCase {
	t.Helper()
	declared := m.Position{File: "mml.yaml", Line: 7}
	ownerAt := placed(
		"orders.sql",
		new(table("orders", []string{"order_id"}, col("order_id", m.TypeInt))),
	)
	ownerAt.Annotations = annAt(declared, "owner", "team:orders")
	staleAt := placed(
		"orders.sql",
		new(table("orders", []string{"order_id"}, col("order_id", m.TypeInt))),
	)
	staleAt.Annotations = annAt(
		declared,
		"definition",
		goodDefinition+" fp=000000000000",
		"owner",
		"team:orders",
		"escalation",
		"role:cto",
	)
	cart := placed(
		"carts.sql",
		new(
			table(
				"carts",
				[]string{"cart_id"},
				col("cart_id", m.TypeInt),
				col("owner", m.TypeString),
			),
		),
	)
	tickets := func(policy string) m.Entity {
		e := placed(
			"tickets.sql",
			new(
				entityOf(
					"tickets",
					[]string{"history", policy},
					col("ticket_id", m.TypeInt),
					col("status", m.TypeString),
				),
			),
		)
		e.PrimaryKey = []string{"ticket_id"}
		return e
	}
	child := func(name string, fields ...m.Field) m.Entity {
		e := placed(
			name+".sql",
			new(entityOf(name, nil, append([]m.Field{col("ticket_id", m.TypeInt)}, fields...)...)),
		)
		e.Refs = []m.Ref{{Fields: []string{"ticket_id"}, Target: "tickets"}}
		return e
	}
	twice := m.Query{
		Kind:      m.QueryCreateView,
		Target:    "by_status",
		Pos:       at(3),
		Relations: []m.Relation{{Name: "tickets", Alias: "t"}},
		GroupBy: []m.ColumnRef{
			{Relation: "t", Column: "status"},
			{Relation: "t", Column: "status"},
		},
	}
	customers := placed(
		"customers.sql",
		new(
			table(
				"customers",
				[]string{"customer_id"},
				col("customer_id", m.TypeInt),
				col("email", m.TypeString),
				col("tier", m.TypeString),
			),
		),
	)
	host := func(name string) m.Entity {
		e := placed(
			name+".sql",
			new(
				table(
					name,
					nil,
					col("customer_id", m.TypeInt),
					col("email", m.TypeString),
					col("customer_tier", m.TypeString),
				),
			),
		)
		e.Refs = []m.Ref{{Fields: []string{"customer_id"}, Target: "customers"}}
		return e
	}
	items := placed(
		"items.sql",
		new(
			entityOf(
				"items",
				[]string{"layer", "reference"},
				col("item_id", m.TypeInt),
				col("pack_qty", m.TypeInt),
			),
		),
	)
	products := placed(
		"products.sql",
		new(table("products", []string{"product_id"}, col("product_id", m.TypeInt))),
	)
	var referrers []m.Entity
	for _, n := range []string{"a", "b", "c", "d", "e"} {
		r := table(n+"_uses", nil, col("x", m.TypeInt))
		r.Refs = []m.Ref{{Fields: []string{"x"}, Target: "products"}}
		referrers = append(referrers, r)
	}
	orders := placed(
		"orders.sql",
		new(
			table(
				"orders",
				[]string{"order_id"},
				col("order_id", m.TypeInt),
				col("order_date", m.TypeDate),
			),
		),
	)
	lines := placed(
		"lines.sql",
		new(table(
			"order_lines",
			[]string{"line_id"},
			col("line_id", m.TypeInt),
			col("order_id", m.TypeInt),
			col(
				"order_note",
				m.TypeString,
			),
			col("order_date", m.TypeDate),
			col("overall_rank", m.TypeInt),
		)),
	)
	lines.Refs = []m.Ref{{Fields: []string{"order_id"}, Target: "orders"}}
	clicks := placed(
		"clicks.sql",
		new(
			table(
				"clicks",
				nil,
				m.Field{Name: "clicked_at", Type: m.TypeTimestamptz, RawType: "timestamptz(0)"},
				m.Field{Name: "shipped_at", Type: m.TypeDate, RawType: "date"},
			),
		),
	)
	return map[string]messageCase{
		"1.1-B keys only": {in: &m.Input{
			Sources: []m.Source{{Entities: []m.Entity{cart}}},
			Lock:    lockOf(t),
			Artifacts: m.Artifacts{
				Logical: []m.LogicalRecord{
					{
						Entity:     "cart",
						Attributes: []string{"cart_id"},
						Keys:       []string{"owner"},
						Pos:        m.Position{File: "model/logical/cart.yaml", Line: 2},
					},
				},
			},
		}, designs: []string{"1.1-B"}},
		"1.13-A at the owner":           {in: src(ownerAt), designs: []string{"1.13-A"}},
		"1.2-A stale at the definition": {in: src(staleAt), designs: []string{"1.2-A"}},
		"3.1-A required and analyzed": {
			in:      withQueries(src(tickets("required")), twice),
			designs: []string{"3.1-A"},
		},
		"3.1-A latest only, one view": {
			in:      withQueries(src(tickets("latest_only")), twice),
			designs: []string{"3.1-A"},
		},
		"3.1-A equal gaps": {
			in: src(tickets("required"), child("ticket_events", col("event_type", m.TypeString)),
				child("ticket_log", col("actor_id", m.TypeInt))),
			designs: []string{"3.1-A"},
		},
		"3.2-A two hosts": {
			in:      src(customers, host("orders"), host("invoices")),
			designs: []string{"3.2-A"},
		},
		"3.4-A unchecked": {in: src(items), designs: []string{"3.4-A"}},
		"3.4-A candidate": {
			in:      src(append([]m.Entity{products}, referrers...)...),
			designs: []string{"3.4-A"},
		},
		"7.4-A three ways": {in: src(orders, lines), designs: []string{"7.4-A"}},
		"9.8-A two ways":   {in: src(clicks), designs: []string{"9.8-A"}},
	}
}
