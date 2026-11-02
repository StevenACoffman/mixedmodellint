package mixedmodellint_test

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

func table(name string, pk []string, fields ...m.Field) m.Entity {
	return m.Entity{
		ID: "sql:" + name, Kind: m.KindTable, Source: m.SourceSQL, Name: name,
		PrimaryKey: pk, Fields: fields, Annotations: m.Annotations{},
		Pos: m.Position{File: "db.sql", Line: 1},
	}
}

func col(name string, t m.LogicalType) m.Field { return m.Field{Name: name, Type: t} }

// lockOf records entities as a lock, through the file format, as the lock
// command and check would.
func lockOf(t *testing.T, entities ...m.Entity) *m.Lock {
	t.Helper()
	var buf bytes.Buffer
	ok(t, m.WriteLock(&buf, m.NewLock(&m.Input{Sources: []m.Source{{Entities: entities}}})))
	l, err := m.ReadLock(&buf)
	ok(t, err)
	return &l
}

// findingsOf runs Analyze and returns "design entity field" for each finding
// of the given designs, sorted.
func findingsOf(in *m.Input, designs ...string) []string {
	r := m.Analyze(in)
	var got []string
	for i := range r.Findings {
		f := &r.Findings[i]
		if slices.Contains(designs, f.Design) {
			got = append(got, strings.TrimSpace(f.Design+" "+f.Entity+" "+f.Field))
		}
	}
	slices.Sort(got)
	return got
}

func TestLockRoundTrip(t *testing.T) {
	t.Parallel()
	orders := table("orders", []string{"order_id"},
		col("order_id", m.TypeInt), col("placed_at", m.TypeTimestamptz))
	orders.Fields[1].Nullable = true
	lines := table("order_lines", []string{"order_id", "line_no"},
		col("order_id", m.TypeInt), col("line_no", m.TypeInt))
	lines.Refs = []m.Ref{{Fields: []string{"order_id"}, Target: "orders"}}
	lines.Uniques = [][]string{{"line_no"}}
	src := []m.Source{{Entities: []m.Entity{orders, lines}}}

	var first, second bytes.Buffer
	ok(t, m.WriteLock(&first, m.NewLock(&m.Input{Sources: src})))
	l, err := m.ReadLock(bytes.NewReader(first.Bytes()))
	ok(t, err)
	ok(t, m.WriteLock(&second, l))
	equals(t, first.String(), second.String())
	if strings.Index(
		first.String(),
		`"sql:order_lines"`,
	) > strings.Index(
		first.String(),
		`"sql:orders"`,
	) {
		t.Fatal("entities are not sorted by ID")
	}
	for _, want := range []string{
		`"type": "timestamptz"`, `"nullable": true`, `"kind": "table"`,
		`"source": "sql"`, `"target": "orders"`, `"uniques"`,
	} {
		if !strings.Contains(first.String(), want) {
			t.Fatalf("lock lacks %s:\n%s", want, first.String())
		}
	}

	var empty bytes.Buffer
	ok(t, m.WriteLock(&empty, m.Lock{}))
	if !strings.Contains(empty.String(), `"entities": []`) {
		t.Fatalf("empty lock should list no entities: %s", empty.String())
	}
}

func TestReadLockErrors(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"not JSON":      "{",
		"wrong version": `{"version": 99, "entities": []}`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := m.ReadLock(strings.NewReader(src))
			equals(t, m.EINVALID, m.ErrorCode(err))
		})
	}
}

func TestLockStale(t *testing.T) {
	t.Parallel()
	base := table("orders", []string{"order_id"}, col("order_id", m.TypeInt),
		col("total", m.TypeDecimal), col("note", m.TypeString))
	retyped := base
	retyped.Fields = []m.Field{
		col("order_id", m.TypeInt), col("total", m.TypeFloat),
		col("note", m.TypeString),
	}
	nullable := base
	nullable.Fields = slices.Clone(base.Fields)
	nullable.Fields[2].Nullable = true
	grown := base
	grown.Fields = append(slices.Clone(base.Fields), col("coupon", m.TypeString))
	shrunk := base
	shrunk.Fields = base.Fields[:2]
	rekeyed := base
	rekeyed.Uniques = [][]string{{"note"}}
	carts := table("carts", []string{"cart_id"}, col("cart_id", m.TypeInt))
	cases := map[string]struct {
		locked, current []m.Entity
		want            string // a fragment of the one message, or "" for none
	}{
		"unchanged": {[]m.Entity{base}, []m.Entity{base}, ""},
		"added entity": {
			[]m.Entity{base},
			[]m.Entity{base, carts},
			"carts is not in mml.lock.json",
		},
		"removed entity": {
			[]m.Entity{base, carts},
			[]m.Entity{base},
			"carts is in mml.lock.json but no longer",
		},
		"field added":       {[]m.Entity{base}, []m.Entity{grown}, "added coupon"},
		"field removed":     {[]m.Entity{base}, []m.Entity{shrunk}, "removed note"},
		"field retyped":     {[]m.Entity{base}, []m.Entity{retyped}, "retyped total"},
		"nullability":       {[]m.Entity{base}, []m.Entity{nullable}, "retyped note"},
		"key roles changed": {[]m.Entity{base}, []m.Entity{rekeyed}, "keys or references changed"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := m.Analyze(&m.Input{
				Sources: []m.Source{{Entities: tc.current}}, Lock: lockOf(t, tc.locked...),
			})
			var msgs []string
			for i := range r.Findings {
				if r.Findings[i].Design == "mml/lock-stale" {
					msgs = append(msgs, r.Findings[i].Measured)
				}
			}
			if tc.want == "" {
				equals(t, 0, len(msgs))
				return
			}
			equals(t, 1, len(msgs))
			if !strings.Contains(msgs[0], tc.want) {
				t.Fatalf("message %q lacks %q", msgs[0], tc.want)
			}
		})
	}
}

func TestNewTableGate(t *testing.T) {
	t.Parallel()
	orders := table("orders", []string{"order_id"}, col("order_id", m.TypeInt))
	carts := table(
		"carts",
		[]string{"cart_id"},
		col("cart_id", m.TypeInt),
		col("owner", m.TypeString),
	)
	with := func(e m.Entity, kv ...string) m.Entity {
		e.Annotations = ann(kv...)
		return e
	}
	view := carts
	view.Kind = m.KindView
	record := func(attrs, keys []string) m.Artifacts {
		return m.Artifacts{Logical: []m.LogicalRecord{{
			Entity: "cart", Attributes: attrs, Keys: keys,
			Pos: m.Position{File: "model/logical/cart.yaml", Line: 1},
		}}}
	}
	cases := map[string]struct {
		current   m.Entity
		artifacts m.Artifacts
		noLock    bool
		want      []string
	}{
		"new table without a record": {current: carts, want: []string{"1.1-B carts"}},
		"no lock, nothing is new":    {current: carts, noLock: true},
		"existing table":             {current: orders},
		"ADR names it": {
			current: carts,
			artifacts: m.Artifacts{
				ADRs: []m.Document{{Path: "docs/adr/7.md", Text: "We add a cart table."}},
			},
		},
		"ADR mentions it only in part": {
			current: carts,
			artifacts: m.Artifacts{
				ADRs: []m.Document{{Path: "docs/adr/7.md", Text: "Shopping cartography."}},
			},
			want: []string{"1.1-B carts"},
		},
		"agreeing logical record": {
			current:   carts,
			artifacts: record([]string{"cart_id", "owner"}, []string{"cart_id"}),
		},
		"record without keys": {current: carts, artifacts: record([]string{"owner"}, nil)},
		"record names a missing column": {
			current: carts, artifacts: record([]string{"cart_id", "shopper"}, []string{"cart_id"}),
			want: []string{"1.1-B carts"},
		},
		"record keys differ": {
			current: carts, artifacts: record([]string{"cart_id"}, []string{"owner"}),
			want: []string{"1.1-B carts"},
		},
		"third-party table": {current: with(carts, "source", "third_party")},
		"legacy table":      {current: with(carts, "source", "legacy")},
		"scratch table":     {current: with(carts, "lifecycle", "scratch")},
		"view":              {current: view},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := &m.Input{
				Sources:   []m.Source{{Entities: []m.Entity{orders, tc.current}}},
				Artifacts: tc.artifacts,
			}
			if !tc.noLock {
				in.Lock = lockOf(t, orders)
			}
			equals(t, tc.want, findingsOf(in, "1.1-B"))
		})
	}
}

func TestKeyRenames(t *testing.T) {
	t.Parallel()
	before := table("customers", []string{"customer_id"},
		col("customer_id", m.TypeInt), col("region_code", m.TypeString))
	before.Refs = []m.Ref{{Fields: []string{"region_code"}, Target: "regions"}}
	renamed := table("customers", []string{"cust_key"},
		col("cust_key", m.TypeInt), col("region_code", m.TypeString))
	renamed.Refs = before.Refs
	retyped := table("customers", []string{"cust_key"},
		col("cust_key", m.TypeUUID), col("region_code", m.TypeString))
	dropped := table("customers", nil, col("region_code", m.TypeString))
	orders := table("orders", []string{"order_id"},
		col("order_id", m.TypeInt), col("customer_id", m.TypeInt))
	orders.Refs = []m.Ref{{Fields: []string{"customer_id"}, Target: "customers"}}
	idMap := table(
		"customer_id_map",
		nil,
		col("cust_key", m.TypeInt),
		col("source_system", m.TypeString),
	)
	declaredMap := table("customer_keys", nil, col("cust_key", m.TypeInt))
	declaredMap.Annotations = ann("role", "identity_map")
	oldNewMap := table(
		"customer_renames",
		nil,
		col("cust_key", m.TypeInt),
		col("customer_id", m.TypeInt),
	)
	rewrite := m.Query{
		Kind: m.QueryUpdate, Target: "customers", Pos: at(4),
		Assignments: []m.Assignment{{Column: "customer_id", Pos: at(4)}},
	}
	plainUpdate := m.Query{
		Kind: m.QueryUpdate, Target: "customers", Pos: at(5),
		Assignments: []m.Assignment{{Column: "region_code", Pos: at(5)}},
	}
	cases := map[string]struct {
		current []m.Entity
		queries []m.Query
		want    []string
	}{
		"key renamed": {
			current: []m.Entity{renamed},
			want:    []string{"4.3-A customers cust_key"},
		},
		"renamed with a source_system map": {current: []m.Entity{renamed, idMap}},
		"renamed with a declared map":      {current: []m.Entity{renamed, declaredMap}},
		"renamed with an old-to-new map":   {current: []m.Entity{renamed, oldNewMap}},
		"key removed, not renamed":         {current: []m.Entity{dropped}},
		"replacement of another type":      {current: []m.Entity{retyped}},
		"unchanged":                        {current: []m.Entity{before}},
		"identifier rewritten in place": {
			current: []m.Entity{before}, queries: []m.Query{rewrite},
			want: []string{"4.3-A customers customer_id"},
		},
		"a child's foreign key is not a map": {
			current: []m.Entity{before, orders}, queries: []m.Query{rewrite},
			want: []string{"4.3-A customers customer_id"},
		},
		"a map for another column does not count": {
			current: []m.Entity{
				before,
				declaredMap,
				table("customer_keys2", nil, col("customer_id", m.TypeInt)),
			},
			queries: []m.Query{rewrite},
			want:    []string{"4.3-A customers customer_id"},
		},
		"non-key update": {current: []m.Entity{before}, queries: []m.Query{plainUpdate}},
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

func TestRenameMessageCountsReferences(t *testing.T) {
	t.Parallel()
	before := table("customers", []string{"customer_id"}, col("customer_id", m.TypeInt))
	after := table("customers", []string{"cust_key"}, col("cust_key", m.TypeInt))
	orders := table(
		"orders",
		[]string{"order_id"},
		col("order_id", m.TypeInt),
		col("cust_key", m.TypeInt),
	)
	orders.Refs = []m.Ref{{Fields: []string{"cust_key"}, Target: "customers"}}
	r := m.Analyze(&m.Input{
		Sources: []m.Source{{Entities: []m.Entity{after, orders}}}, Lock: lockOf(t, before),
	})
	var msg string
	for i := range r.Findings {
		if r.Findings[i].Design == "4.3-A" {
			msg = r.Findings[i].Measured
		}
	}
	if !strings.Contains(msg, "renamed to cust_key since mml.lock.json, and 1 entity references") {
		t.Fatalf("unexpected message %q", msg)
	}
}

func TestRemovalAfterAggregation(t *testing.T) {
	t.Parallel()
	clicks := table("clicks", []string{"click_id"}, col("click_id", m.TypeInt),
		col("clicked_at", m.TypeTimestamptz))
	clicks.Annotations = ann("layer", "event")
	plain := table(
		"carts",
		[]string{"cart_id"},
		col("cart_id", m.TypeInt),
		col("created_at", m.TypeTimestamptz),
	)
	vendor := table("vendor_clicks", []string{"click_id"}, col("click_id", m.TypeInt))
	vendor.Annotations = ann("source", "third_party")
	ownedUp := table("click_archive", []string{"click_id"}, col("click_id", m.TypeInt))
	recoverable := clicks
	recoverable.Annotations = ann("layer", "event", "recoverable_from", "vendor_clicks")
	wrongUp := clicks
	wrongUp.Annotations = ann("layer", "event", "recoverable_from", "click_archive")
	daily := m.Query{
		Kind: m.QueryCreateView, Target: "daily_clicks", Pos: at(2),
		Relations:  []m.Relation{{Name: "clicks", Alias: "c"}},
		Aggregates: []m.Aggregate{{Func: "count", Star: true}},
	}
	copyView := m.Query{
		Kind: m.QueryCreateView, Target: "click_copy", Pos: at(2),
		Relations: []m.Relation{{Name: "clicks", Alias: "c"}},
	}
	drop := m.Query{Kind: m.QueryDrop, Target: "clicks", Pos: at(9)}
	truncate := m.Query{Kind: m.QueryTruncate, Target: "clicks", Pos: at(9)}
	retention := func(target, column string) m.Query {
		return m.Query{
			Kind: m.QueryDelete, Target: target, Pos: at(9),
			Filters: []m.ColumnRef{{Relation: target, Column: column}},
		}
	}
	cartView := daily
	cartView.Relations = []m.Relation{{Name: "carts", Alias: "c"}}
	cases := map[string]struct {
		locked, current []m.Entity
		queries         []m.Query
		sidecar         m.Sidecar
		want            []string
	}{
		"removed since the lock while summarized": {
			locked: []m.Entity{clicks}, queries: []m.Query{daily}, want: []string{"7.5-B clicks"},
		},
		"removed with nothing summarizing it": {locked: []m.Entity{clicks}},
		"summarized only by a plain view": {
			locked: []m.Entity{clicks}, queries: []m.Query{copyView},
		},
		"dropped in a migration": {
			queries: []m.Query{daily, drop}, want: []string{"7.5-B"},
		},
		"truncated": {
			current: []m.Entity{clicks}, queries: []m.Query{daily, truncate},
			want: []string{"7.5-B clicks"},
		},
		"retention delete on event time": {
			current: []m.Entity{
				clicks,
			},
			queries: []m.Query{daily, retention("clicks", "clicked_at")},
			want:    []string{"7.5-B clicks"},
		},
		"delete by key": {
			current: []m.Entity{clicks}, queries: []m.Query{daily, retention("clicks", "click_id")},
		},
		"delete from a table that is not events": {
			current: []m.Entity{
				plain,
			},
			queries: []m.Query{cartView, retention("carts", "created_at")},
		},
		"recoverable from a third-party upstream": {
			current: []m.Entity{recoverable, vendor}, queries: []m.Query{daily, truncate},
		},
		"recoverable_from naming owned data": {
			current: []m.Entity{wrongUp, ownedUp}, queries: []m.Query{daily, truncate},
			want: []string{"7.5-B clicks"},
		},
		"dropped table declares recovery in mml.yaml": {
			current: []m.Entity{vendor}, queries: []m.Query{daily, drop},
			sidecar: m.Sidecar{Entities: map[string]m.SidecarEntity{
				"clicks": {Annotations: ann("recoverable_from", "vendor_clicks")},
			}},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := &m.Input{
				Sources: []m.Source{{Entities: tc.current, Queries: tc.queries}},
				Sidecar: tc.sidecar,
			}
			if tc.locked != nil {
				in.Lock = lockOf(t, tc.locked...)
			}
			equals(t, tc.want, findingsOf(in, "7.5-B"))
		})
	}
}

// TestLockRecordsSidecarDeclarations pins that a lock reads the model as
// check does: a reference declared in mml.yaml is in the lock, so the lock is
// not stale the moment it is written.
func TestLockRecordsSidecarDeclarations(t *testing.T) {
	t.Parallel()
	event := table(
		"order_placed",
		[]string{"event_id"},
		col("event_id", m.TypeInt),
		col("order_id", m.TypeInt),
	)
	in := &m.Input{
		Sources: []m.Source{{Entities: []m.Entity{event}}},
		Sidecar: m.Sidecar{Entities: map[string]m.SidecarEntity{
			"order_placed": {Fields: map[string]m.Annotations{"order_id": ann("ref", "orders")}},
		}},
	}
	var buf bytes.Buffer
	ok(t, m.WriteLock(&buf, m.NewLock(in)))
	if !strings.Contains(buf.String(), `"target": "orders"`) {
		t.Fatalf("lock lacks the declared reference:\n%s", buf.String())
	}
	l, err := m.ReadLock(&buf)
	ok(t, err)
	in.Lock = &l
	equals(t, 0, countDesign(m.Analyze(in), "mml/lock-stale"))
}

// TestBaseIsThePreviousModel pins that --base's model is what the change is
// compared with, ahead of the lock, which still reports itself stale.
func TestBaseIsThePreviousModel(t *testing.T) {
	t.Parallel()
	orders := table("orders", []string{"order_id"}, col("order_id", m.TypeInt))
	carts := table("carts", []string{"cart_id"}, col("cart_id", m.TypeInt))
	base := &m.Input{Sources: []m.Source{{Entities: []m.Entity{orders}}}}
	in := &m.Input{Sources: []m.Source{{Entities: []m.Entity{orders, carts}}}, Base: base}
	equals(t, []string{"1.1-B carts"}, findingsOf(in, "1.1-B", "mml/lock-stale"))

	// A lock that already has carts does not hide that carts is new since the
	// base, and still reports itself stale for orders' new field.
	grown := table(
		"orders",
		[]string{"order_id"},
		col("order_id", m.TypeInt),
		col("note", m.TypeString),
	)
	in = &m.Input{
		Sources: []m.Source{{Entities: []m.Entity{grown, carts}}},
		Lock:    lockOf(t, orders, carts), Base: base,
	}
	equals(
		t,
		[]string{"1.1-B carts", "mml/lock-stale orders"},
		findingsOf(in, "1.1-B", "mml/lock-stale"),
	)
}
