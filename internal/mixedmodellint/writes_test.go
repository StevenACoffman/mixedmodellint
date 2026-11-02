package mixedmodellint_test

import (
	"slices"
	"strings"
	"testing"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// writeCatalog is a parent, a child and a grandchild linked by references,
// and an event table, for the designs that read writes.
func writeCatalog() []m.Entity {
	o := orders()
	o.Fields = append(o.Fields, m.Field{Name: "shipping_fee", Type: m.TypeDecimal})
	return []m.Entity{
		o,
		{
			ID: "sql:order_lines", Kind: m.KindTable, Source: m.SourceSQL, Name: "order_lines",
			Annotations: ann("grain", "one row per order line"),
			PrimaryKey:  []string{"order_id", "line_no"},
			Refs:        []m.Ref{{Fields: []string{"order_id"}, Target: "orders"}},
			Fields: []m.Field{
				{Name: "order_id", Type: m.TypeInt},
				{Name: "line_no", Type: m.TypeInt},
				{Name: "order_fee", Type: m.TypeDecimal},
				{
					Name: "unit_price", Type: m.TypeDecimal,
					Annotations: ann("captured_at_transaction", "true"),
				},
				{
					Name: "fee_copy", Type: m.TypeDecimal,
					Annotations: ann("denormalized_for", "access_pattern: invoice"),
				},
			},
		},
		{
			ID: "sql:line_events", Kind: m.KindTable, Source: m.SourceSQL, Name: "line_events",
			PrimaryKey: []string{"event_id"},
			Refs:       []m.Ref{{Fields: []string{"order_id", "line_no"}, Target: "order_lines"}},
			Fields: []m.Field{
				{Name: "event_id", Type: m.TypeInt},
				{Name: "order_id", Type: m.TypeInt},
				{Name: "line_no", Type: m.TypeInt},
				{Name: "fee", Type: m.TypeDecimal},
			},
		},
		{
			ID: "sql:clicks", Kind: m.KindTable, Source: m.SourceSQL, Name: "clicks",
			PrimaryKey: []string{"click_id"},
			Fields: []m.Field{
				{Name: "click_id", Type: m.TypeInt},
				{Name: "clicked_at", Type: m.TypeTimestamptz},
				{Name: "loaded_at", Type: m.TypeTimestamptz},
				{Name: "created_at", Type: m.TypeTimestamptz},
				{Name: "seen_at", Type: m.TypeTimestamptz, Annotations: ann("precision", "us")},
				{
					Name:        "row_created_at",
					Type:        m.TypeTimestamptz,
					Annotations: ann("time", "event"),
				},
				{Name: "clicked_on", Type: m.TypeDate},
				{Name: "shipped_at", Type: m.TypeDate},
				{Name: "expiry", Type: m.TypeTimestamptz},
				{Name: "nano_at", Type: m.TypeTimestamptz, Annotations: ann("precision", "ns")},
				{Name: "coarse_at", Type: m.TypeTimestamptz, Annotations: ann("precision", "s")},
				{Name: "viewed_at", Type: m.TypeTimestampNaive},
				{Name: "ts", Type: m.TypeTimestamptz},
				{Name: "_", Type: m.TypeTimestamptz},
			},
		},
		// teams reaches leagues, and leagues and divisions reference each other
		// and teams: cycles ancestors must end.
		{
			ID: "sql:teams", Kind: m.KindTable, Source: m.SourceSQL, Name: "teams",
			PrimaryKey: []string{"team_id"},
			Refs:       []m.Ref{{Fields: []string{"league_id"}, Target: "leagues"}},
			Fields: []m.Field{
				{Name: "team_id", Type: m.TypeInt},
				{Name: "league_id", Type: m.TypeInt},
				{Name: "fee", Type: m.TypeDecimal},
			},
		},
		{
			ID: "sql:leagues", Kind: m.KindTable, Source: m.SourceSQL, Name: "leagues",
			PrimaryKey: []string{"league_id"},
			Refs: []m.Ref{
				{Fields: []string{"champion_id"}, Target: "teams"},
				{Fields: []string{"division_id"}, Target: "divisions"},
			},
			Fields: []m.Field{
				{Name: "league_id", Type: m.TypeInt},
				{Name: "champion_id", Type: m.TypeInt},
				{Name: "division_id", Type: m.TypeInt},
				{Name: "fee", Type: m.TypeDecimal},
			},
		},
		{
			ID: "sql:divisions", Kind: m.KindTable, Source: m.SourceSQL, Name: "divisions",
			PrimaryKey: []string{"division_id"},
			Refs:       []m.Ref{{Fields: []string{"top_league_id"}, Target: "leagues"}},
			Fields: []m.Field{
				{Name: "division_id", Type: m.TypeInt},
				{Name: "top_league_id", Type: m.TypeInt},
				{Name: "fee", Type: m.TypeDecimal},
			},
		},
	}
}

func at(line int) m.Position { return m.Position{File: "q.sql", Line: line} }

// insert builds a write of one column with the given origins, at line.
func insert(target, column string, line int, origins ...m.Origin) m.Query {
	return m.Query{
		Kind: m.QueryInsert, Target: target, Pos: at(line),
		Assignments: []m.Assignment{{Column: column, Origins: origins, Pos: at(line)}},
	}
}

func copyOf(entity, field string) m.Origin {
	return m.Origin{From: m.FieldRef{Entity: entity, Field: field}}
}

func truncTo(unit string) m.Origin { return m.Origin{Truncate: unit} }

// askFor builds a SELECT from clicks asking for unit from column, at line.
func askFor(unit, column string, line int) m.Query {
	return m.Query{
		Kind: m.QuerySelect, Pos: at(line),
		Relations: []m.Relation{{Name: "clicks", Alias: "c"}},
		TimeExprs: []m.TimeExpr{{
			Func: "date_trunc", Unit: unit, Pos: at(line),
			Column: m.ColumnRef{Relation: "c", Column: column},
		}},
	}
}

func TestWriteDesigns(t *testing.T) {
	t.Parallel()
	goSite := m.Position{File: "store/store.go", Line: 20, Col: 9}
	fromGo := copyOf("Order", "shipping_fee")
	fromGo.Pos = goSite
	cases := map[string]struct {
		queries []m.Query
		want    []string
	}{
		"7.4-D SQL copy from the parent": {
			queries: []m.Query{
				insert("order_lines", "order_fee", 3, copyOf("orders", "shipping_fee")),
			},
			want: []string{"7.4-D order_fee q.sql:3"},
		},
		"7.4-D Go copy points at the Go line": {
			queries: []m.Query{insert("order_lines", "order_fee", 3, fromGo)},
			want:    []string{"7.4-D order_fee store/store.go:20:9"},
		},
		"7.4-D copy from a grandparent": {
			queries: []m.Query{insert("line_events", "fee", 4, copyOf("orders", "shipping_fee"))},
			want:    []string{"7.4-D fee q.sql:4"},
		},
		"7.4-D same copy reported once": {
			queries: []m.Query{insert("order_lines", "order_fee", 3,
				copyOf("orders", "shipping_fee"), copyOf("orders", "shipping_fee"))},
			want: []string{"7.4-D order_fee q.sql:3"},
		},
		"7.4-D reference column may copy its parent key": {
			queries: []m.Query{insert("order_lines", "order_id", 3, copyOf("orders", "order_id"))},
		},
		"7.4-D snapshot taken at the transaction": {
			queries: []m.Query{
				insert("order_lines", "unit_price", 3, copyOf("orders", "shipping_fee")),
			},
		},
		"7.4-D declared denormalization": {
			queries: []m.Query{
				insert("order_lines", "fee_copy", 3, copyOf("orders", "shipping_fee")),
			},
		},
		"7.4-D copy from an entity that is not an ancestor": {
			queries: []m.Query{
				insert("orders", "shipping_fee", 3, copyOf("order_lines", "order_fee")),
			},
		},
		"7.4-D write to a table outside the repository": {
			queries: []m.Query{
				insert("archive", "fee", 3, copyOf("orders", "shipping_fee")),
				insert("order_lines", "order_fee", 4, copyOf("orders", "shipping_fee")),
			},
			want: []string{"7.4-D order_fee q.sql:4"},
		},
		"7.4-D a read is not a write": {
			queries: []m.Query{{
				Kind: m.QuerySelect, Target: "order_lines", Pos: at(3),
				Assignments: []m.Assignment{{
					Column: "order_fee", Pos: at(3),
					Origins: []m.Origin{copyOf("orders", "shipping_fee")},
				}},
			}},
		},
		"7.4-D copy across a reference cycle": {
			queries: []m.Query{insert("teams", "fee", 3, copyOf("leagues", "fee"))},
			want:    []string{"7.4-D fee q.sql:3"},
		},
		"7.4-D copy from beyond a cycle that excludes the writer": {
			queries: []m.Query{insert("teams", "fee", 3, copyOf("divisions", "fee"))},
			want:    []string{"7.4-D fee q.sql:3"},
		},
		"7.4-D an entity is not its own ancestor": {
			queries: []m.Query{insert("teams", "fee", 3, copyOf("teams", "fee"))},
		},
		"9.8-B event time truncated to the second": {
			queries: []m.Query{insert("clicks", "clicked_at", 5, truncTo("second"))},
			want:    []string{"9.8-B clicked_at q.sql:5"},
		},
		"9.8-B milliseconds kept by default": {
			queries: []m.Query{insert("clicks", "clicked_at", 5, truncTo("millisecond"))},
		},
		"9.8-B milliseconds lose declared microseconds": {
			queries: []m.Query{insert("clicks", "seen_at", 5, truncTo("millisecond"))},
			want:    []string{"9.8-B seen_at q.sql:5"},
		},
		"9.8-B ingestion time is not event time": {
			queries: []m.Query{insert("clicks", "loaded_at", 5, truncTo("minute"))},
		},
		"9.8-B created_at is ambiguous": {
			queries: []m.Query{insert("clicks", "created_at", 5, truncTo("minute"))},
		},
		"9.8-B annotation makes a field event time": {
			queries: []m.Query{insert("clicks", "row_created_at", 5, truncTo("minute"))},
			want:    []string{"9.8-B row_created_at q.sql:5"},
		},
		"9.8-B a date named like event time": {
			queries: []m.Query{insert("clicks", "shipped_at", 5, truncTo("day"))},
		},
		"9.8-B a timestamp not named as a time": {
			queries: []m.Query{insert("clicks", "expiry", 5, truncTo("minute"))},
		},
		"9.8-B microseconds lose declared nanoseconds": {
			queries: []m.Query{insert("clicks", "nano_at", 5, truncTo("microsecond"))},
			want:    []string{"9.8-B nano_at q.sql:5"},
		},
		"9.8-B microseconds keep declared microseconds": {
			queries: []m.Query{insert("clicks", "seen_at", 5, truncTo("microsecond"))},
		},
		"9.8-B nanoseconds keep declared nanoseconds": {
			queries: []m.Query{insert("clicks", "nano_at", 5, truncTo("nanosecond"))},
		},
		"9.8-B a naive timestamp is still event time": {
			queries: []m.Query{insert("clicks", "viewed_at", 5, truncTo("second"))},
			want:    []string{"9.8-B viewed_at q.sql:5"},
		},
		"9.8-B a one-word time name": {
			queries: []m.Query{insert("clicks", "ts", 5, truncTo("second"))},
			want:    []string{"9.8-B ts q.sql:5"},
		},
		"9.8-B a name with no words": {
			queries: []m.Query{insert("clicks", "_", 5, truncTo("second"))},
		},
		"9.8-B seconds keep declared seconds": {
			queries: []m.Query{insert("clicks", "coarse_at", 5, truncTo("second"))},
		},
		"9.8-B minutes lose declared seconds": {
			queries: []m.Query{insert("clicks", "coarse_at", 5, truncTo("minute"))},
			want:    []string{"9.8-B coarse_at q.sql:5"},
		},
		"9.8-B a date is not truncated event time": {
			queries: []m.Query{insert("clicks", "clicked_on", 5, truncTo("day"))},
		},
		"7.5-D finer than every write keeps": {
			queries: []m.Query{
				insert("clicks", "clicked_at", 5, truncTo("minute")),
				insert("clicks", "clicked_at", 6, truncTo("hour")),
				askFor("second", "clicked_at", 9),
			},
			want: []string{
				"7.5-D clicked_at q.sql:9", "9.8-B clicked_at q.sql:5", "9.8-B clicked_at q.sql:6",
			},
		},
		"7.5-D the unit the writes keep": {
			queries: []m.Query{
				insert("clicks", "clicked_at", 5, truncTo("minute")),
				askFor("minute", "clicked_at", 9),
			},
			want: []string{"9.8-B clicked_at q.sql:5"},
		},
		"7.5-D one write keeps full precision": {
			queries: []m.Query{
				insert("clicks", "clicked_at", 5, truncTo("minute")),
				insert("clicks", "clicked_at", 6, m.Origin{}),
				askFor("second", "clicked_at", 9),
			},
			want: []string{"9.8-B clicked_at q.sql:5"},
		},
		"7.5-D the finest unit any write keeps": {
			queries: []m.Query{
				insert("clicks", "clicked_at", 5, truncTo("hour")),
				insert("clicks", "clicked_at", 6, truncTo("minute")),
				askFor("minute", "clicked_at", 9),
			},
			want: []string{"9.8-B clicked_at q.sql:5", "9.8-B clicked_at q.sql:6"},
		},
		"7.5-D a full-precision write before a truncating one": {
			queries: []m.Query{
				insert("clicks", "clicked_at", 5, m.Origin{}),
				insert("clicks", "clicked_at", 6, truncTo("minute")),
				askFor("second", "clicked_at", 9),
			},
			want: []string{"9.8-B clicked_at q.sql:6"},
		},
		"7.5-D an unknown write among truncating ones": {
			queries: []m.Query{
				insert("clicks", "clicked_at", 5, truncTo("minute")),
				insert("clicks", "clicked_at", 6),
				askFor("second", "clicked_at", 9),
			},
			want: []string{"9.8-B clicked_at q.sql:5"},
		},
		"7.5-D writes to other columns and entities do not count": {
			queries: []m.Query{
				insert("clicks", "loaded_at", 6),
				insert("orders", "clicked_at", 7),
				insert("clicks", "clicked_at", 5, truncTo("minute")),
				askFor("second", "clicked_at", 9),
			},
			want: []string{"7.5-D clicked_at q.sql:9", "9.8-B clicked_at q.sql:5"},
		},
		"7.5-D a write with unknown origin": {
			queries: []m.Query{
				insert("clicks", "clicked_at", 5),
				askFor("second", "clicked_at", 9),
			},
		},
		"7.5-D a date column still holds days": {
			queries: []m.Query{askFor("hour", "clicked_on", 7)},
			want:    []string{"7.5-D clicked_on q.sql:7"},
		},
		"7.5-D a timestamp never written": {
			queries: []m.Query{askFor("second", "clicked_at", 9)},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := m.Analyze(&m.Input{Sources: []m.Source{{
				Entities: writeCatalog(), Queries: tc.queries,
			}}})
			var got []string
			for i := range r.Findings {
				f := &r.Findings[i]
				switch f.Design {
				case "7.4-D", "7.5-D", "9.8-B":
					got = append(got, f.Design+" "+f.Field+" "+f.Pos.String())
				}
			}
			slices.Sort(got)
			equals(t, tc.want, got)
		})
	}
}

// TestTruncatedColumnNamesTheWrite pins which write a 7.5-D finding about a
// truncated timestamp cites: the first one that truncates to the unit kept.
func TestTruncatedColumnNamesTheWrite(t *testing.T) {
	t.Parallel()
	r := m.Analyze(&m.Input{Sources: []m.Source{{
		Entities: writeCatalog(),
		Queries: []m.Query{
			insert("clicks", "clicked_at", 5, truncTo("hour")),
			insert("clicks", "clicked_at", 6, truncTo("minute")),
			insert("clicks", "clicked_at", 7, truncTo("minute")),
			askFor("second", "clicked_at", 9),
		},
	}}})
	var measured []string
	for i := range r.Findings {
		if r.Findings[i].Design == "7.5-D" {
			measured = append(measured, r.Findings[i].Measured)
		}
	}
	equals(t, 1, len(measured))
	if !strings.Contains(measured[0], "truncates it to the minute (as at q.sql:6)") {
		t.Fatalf("finding does not cite the first minute write: %s", measured[0])
	}
}
