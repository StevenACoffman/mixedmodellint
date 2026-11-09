package postgres_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	pg "github.com/pganalyze/pg_query_go/v6"
	pgquery "github.com/wasilibs/go-pgquery"

	mml "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
	"github.com/StevenACoffman/mixedmodellint/internal/postgres"
)

func equals(t *testing.T, exp, act any) {
	t.Helper()
	if !reflect.DeepEqual(exp, act) {
		t.Fatalf("expected %#v, got %#v", exp, act)
	}
}

func loadFixtures(t *testing.T) mml.Source {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("testdata", "migrations", "*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	var files []postgres.File
	for _, p := range paths {
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, postgres.File{Path: p, Src: src})
	}
	return postgres.Load(files, nil)
}

func entity(t *testing.T, src mml.Source, name string) mml.Entity {
	t.Helper()
	for i := range src.Entities {
		if src.Entities[i].Name == name {
			return src.Entities[i]
		}
	}
	t.Fatalf("no entity %q", name)
	return mml.Entity{}
}

func TestLoadReplaysMigrations(t *testing.T) {
	t.Parallel()
	src := loadFixtures(t)
	orders := entity(t, src, "orders")
	lines := entity(t, src, "order_lines")
	var orderFields []string
	for _, f := range orders.Fields {
		orderFields = append(orderFields, f.Name)
	}
	placed, _ := orders.Field("placed_at")
	amount, _ := lines.Field("amount")
	grain, _ := orders.Annotations.Get("grain")
	layer, _ := orders.Annotations.Get("layer")
	unit, _ := amount.Annotations.Get("unit")

	cases := map[string]struct{ exp, act any }{
		"columns after ADD and DROP": {
			[]string{"order_id", "customer_id", "placed_at", "coupon_code"}, orderFields,
		},
		"ALTER TYPE applied":          {mml.TypeTimestamptz, placed.Type},
		"column primary key":          {[]string{"order_id"}, orders.PrimaryKey},
		"table primary key":           {[]string{"order_id", "line_no"}, lines.PrimaryKey},
		"foreign key":                 {"orders", lines.Refs[0].Target},
		"qualified COMMENT ON finds":  {"one row per order", grain},
		"directive above CREATE":      {"oltp", layer},
		"column comment annotation":   {"USD", unit},
		"raw type keeps modifiers":    {"numeric(12,2)", amount.RawType},
		"primary key is not nullable": {false, lines.Fields[0].Nullable},
		"entity position at name":     {3, orders.Pos.Line},
		"parse error becomes problem": {1, len(src.Problems)},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.exp, tc.act)
		})
	}
}

func TestLoadSnippets(t *testing.T) {
	t.Parallel()
	at := mml.Position{File: "store/store.go", Line: 35, Col: 30}
	src := postgres.Load(nil, []postgres.Snippet{
		{
			SQL:    "SELECT o.id FROM orders o JOIN order_lines l ON l.order_id = o.order_id",
			Pos:    at,
			Strict: true,
		},
		{SQL: "not sql at all", Pos: at, Strict: false},
		{SQL: "SELEC broken", Pos: at, Strict: true},
	})
	cases := map[string]struct{ exp, act any }{
		"one query from the valid snippet": {1, len(src.Queries)},
		"query at the call site":           {at, src.Queries[0].Pos},
		"join at the call site":            {at, src.Queries[0].Joins[0].Pos},
		"strict parse failure reported":    {1, len(src.Problems)},
		"problem at the call site":         {at, src.Problems[0].Pos},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.exp, tc.act)
		})
	}
}

func TestAssignments(t *testing.T) {
	t.Parallel()
	goPos := mml.Position{File: "store/store.go", Line: 20, Col: 9}
	fee := []mml.Origin{{From: mml.FieldRef{Entity: "Order", Field: "shipping_fee"}, Pos: goPos}}
	copied := func(entity, field string) []mml.Origin {
		return []mml.Origin{{From: mml.FieldRef{Entity: entity, Field: field}}}
	}
	cases := map[string]struct {
		sql  string
		args [][]mml.Origin
		want []mml.Assignment
	}{
		"insert select copies a joined column": {
			sql: "INSERT INTO order_lines (order_id, order_fee) " +
				"SELECT o.order_id, o.shipping_fee FROM orders o JOIN carts c ON c.id = o.cart_id",
			want: []mml.Assignment{
				{Column: "order_id", Origins: copied("orders", "order_id")},
				{Column: "order_fee", Origins: copied("orders", "shipping_fee")},
			},
		},
		"unqualified column with one relation": {
			sql: "INSERT INTO daily (day) SELECT placed_at::date FROM orders",
			want: []mml.Assignment{
				{
					Column: "day",
					Origins: []mml.Origin{
						{From: mml.FieldRef{Entity: "orders", Field: "placed_at"}, Truncate: "day"},
					},
				},
			},
		},
		"update from copies the parent": {
			sql: "UPDATE order_lines l SET order_fee = o.shipping_fee, qty = 1 " +
				"FROM orders o WHERE o.order_id = l.order_id",
			want: []mml.Assignment{
				{Column: "order_fee", Origins: copied("orders", "shipping_fee")},
				{Column: "qty"},
			},
		},
		"bind parameters take the Go origins": {
			sql:  "INSERT INTO order_lines (order_fee, qty) VALUES ($1, $2)",
			args: [][]mml.Origin{fee},
			want: []mml.Assignment{{Column: "order_fee", Origins: fee}, {Column: "qty"}},
		},
		"date_trunc and precision casts truncate": {
			sql: "INSERT INTO events (a, b, c, d) VALUES " +
				"(date_trunc('minute', now()), now()::timestamptz(0), now()::timestamp(3), now()::timestamptz)",
			want: []mml.Assignment{
				{Column: "a", Origins: []mml.Origin{{Truncate: "minute"}}},
				{Column: "b", Origins: []mml.Origin{{Truncate: "second"}}},
				{Column: "c", Origins: []mml.Origin{{Truncate: "millisecond"}}},
				{Column: "d"},
			},
		},
		"rows of VALUES merge": {
			sql: "INSERT INTO events (a) VALUES (date_trunc('hour', now())), (date_trunc('day', now()))",
			want: []mml.Assignment{
				{Column: "a", Origins: []mml.Origin{{Truncate: "hour"}, {Truncate: "day"}}},
			},
		},
		"column of a CTE is not a stored field": {
			sql:  "WITH x AS (SELECT 1 AS v) INSERT INTO t (v) SELECT x.v FROM x",
			want: []mml.Assignment{{Column: "v"}},
		},
		"no column list": {sql: "INSERT INTO t VALUES (1)"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			at := mml.Position{File: "q.go", Line: 1}
			src := postgres.Load(
				nil,
				[]postgres.Snippet{{SQL: tc.sql, Pos: at, Strict: true, Args: tc.args}},
			)
			equals(t, 0, len(src.Problems))
			got := src.Queries[len(src.Queries)-1].Assignments
			for i := range tc.want {
				tc.want[i].Pos = at
			}
			equals(t, tc.want, got)
		})
	}
}

func TestSQLCQueriesMerge(t *testing.T) {
	t.Parallel()
	file := postgres.File{Path: "query.sql", Src: []byte(
		"-- name: GetOrder :one\nSELECT * FROM orders WHERE order_id = @order_id;\n\n" +
			"-- name: CreateLine :exec\nINSERT INTO order_lines (order_id, order_fee)\n" +
			"VALUES (@order_id, sqlc.arg(order_fee));\n",
	)}
	goPos := mml.Position{File: "db/query.sql.go", Line: 40}
	fee := []mml.Origin{{From: mml.FieldRef{Entity: "Order", Field: "shipping_fee"}, Pos: goPos}}
	src := postgres.Load([]postgres.File{file}, []postgres.Snippet{
		{
			SQL: "-- name: CreateLine :exec\nINSERT INTO order_lines (order_id, order_fee)\n" +
				"VALUES ($1, $2)\n",
			Pos: goPos, Strict: true, Args: [][]mml.Origin{nil, fee},
		},
		{SQL: "-- name: Other :exec\nDELETE FROM orders", Pos: goPos, Strict: true},
	})
	write := src.Queries[1]
	cases := map[string]struct{ exp, act any }{
		"generated copy merged, unnamed kept": {3, len(src.Queries)},
		"merged query stays in the .sql file": {"query.sql", write.Pos.File},
		"merged query takes the Go origins":   {fee, write.Assignments[1].Origins},
		"unbound parameter stays unknown":     {[]mml.Origin(nil), write.Assignments[0].Origins},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.exp, tc.act)
		})
	}
}

func TestRenamesAndRemovals(t *testing.T) {
	t.Parallel()
	src := postgres.Load([]postgres.File{{Path: "001.sql", Src: []byte(`
CREATE TABLE customers (
    customer_id BIGINT PRIMARY KEY,
    region TEXT CHECK (region IN ('eu', 'us'))
);
CREATE TABLE orders (order_id BIGINT PRIMARY KEY, customer_id BIGINT REFERENCES customers);
ALTER TABLE customers RENAME COLUMN customer_id TO cust_key;
ALTER TABLE customers RENAME COLUMN region TO region_code;
ALTER TABLE orders RENAME COLUMN customer_id TO cust_key;
ALTER TABLE customers RENAME TO clients;
ALTER TABLE missing RENAME COLUMN a TO b;
CREATE TABLE clicks (id BIGINT, clicked_at TIMESTAMPTZ);
TRUNCATE clicks;
DELETE FROM clicks c WHERE c.clicked_at < now() - interval '90 days';
DROP TABLE clicks;
`)}}, nil)
	var names []string
	for i := range src.Entities {
		names = append(names, src.Entities[i].ID)
	}
	clients := entity(t, src, "clients")
	orders := entity(t, src, "orders")
	var kinds []mml.QueryKind
	for i := range src.Queries {
		kinds = append(kinds, src.Queries[i].Kind)
	}
	cases := map[string]struct{ exp, act any }{
		"table renamed, dropped table gone": {[]string{"sql:orders", "sql:clients"}, names},
		"renamed key follows":               {[]string{"cust_key"}, clients.PrimaryKey},
		"renamed column":                    {"region_code", clients.Fields[1].Name},
		"check follows the column":          {"region_code", clients.Checks[0].Field},
		"reference column follows":          {[]string{"cust_key"}, orders.Refs[0].Fields},
		"removals are queries": {
			[]mml.QueryKind{mml.QueryTruncate, mml.QueryDelete, mml.QueryDrop}, kinds,
		},
		"truncate target": {"clicks", src.Queries[0].Target},
		"delete filters": {
			[]mml.ColumnRef{{Relation: "c", Column: "clicked_at"}},
			src.Queries[1].Filters,
		},
		"drop target":       {"clicks", src.Queries[2].Target},
		"no parse problems": {0, len(src.Problems)},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.exp, tc.act)
		})
	}
}

// TestKeywordListMatchesTheParser checks the generated keyword list that
// §5.1-A reads against the parser itself: each listed word must scan as a
// keyword that cannot be a column name unquoted, and words the rule says are
// allowed must not.
func TestKeywordListMatchesTheParser(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(
		filepath.Join("..", "mixedmodellint", "lexicon", "postgres_keywords.txt"),
	)
	if err != nil {
		t.Fatal(err)
	}
	kind := func(word string) pg.KeywordKind {
		res, err := pgquery.Scan(word)
		if err != nil || len(res.GetTokens()) != 1 {
			t.Fatalf("scan %q: %v", word, err)
		}
		return res.GetTokens()[0].GetKeywordKind()
	}
	for _, w := range strings.Fields(string(src)) {
		if k := kind(
			w,
		); k != pg.KeywordKind_RESERVED_KEYWORD &&
			k != pg.KeywordKind_TYPE_FUNC_NAME_KEYWORD {
			t.Errorf("%s is listed as reserved but scans as %s", w, k)
		}
	}
	for _, w := range []string{"date", "timestamp", "type", "status", "name", "value"} {
		if k := kind(
			w,
		); k == pg.KeywordKind_RESERVED_KEYWORD ||
			k == pg.KeywordKind_TYPE_FUNC_NAME_KEYWORD {
			t.Errorf("%s scans as %s, but the rule allows it as a column name", w, k)
		}
	}
}

func TestComparisonChecks(t *testing.T) {
	t.Parallel()
	src := postgres.Load([]postgres.File{{Path: "001.sql", Src: []byte(`
CREATE TABLE items (
    pack_qty   INTEGER CHECK (pack_qty > 0),
    net_weight NUMERIC CHECK (net_weight BETWEEN 0 AND 100),
    grade      TEXT CHECK (grade IN ('a', 'b')),
    note       TEXT CHECK (length(note) < 200)
);
`)}}, nil)
	equals(t, []mml.Check{
		{Field: "pack_qty", Expr: "pack_qty > 0"},
		{Field: "net_weight", Expr: "net_weight >= 0 AND net_weight <= 100"},
		{Field: "grade", Enum: []string{"a", "b"}},
	}, entity(t, src, "items").Checks)
}

func TestLoadGrants(t *testing.T) {
	t.Parallel()
	ddl := "CREATE TABLE orders (order_id BIGINT PRIMARY KEY);\n" +
		"CREATE TABLE sales.refunds (refund_id BIGINT PRIMARY KEY);\n"
	cases := map[string]struct {
		sql   string
		table string
		want  []mml.Grant
	}{
		"privileges to a role": {
			sql:   "GRANT INSERT, UPDATE ON orders TO support_agent;",
			table: "orders",
			want: []mml.Grant{{
				Role: "support_agent", Privileges: []string{"INSERT", "UPDATE"},
				Pos: mml.Position{File: "grants.sql", Line: 3, Col: 1},
			}},
		},
		"ALL expands": {
			sql:   "GRANT ALL ON TABLE sales.refunds TO llm_writer;",
			table: "refunds",
			want: []mml.Grant{{
				Role: "llm_writer",
				Privileges: []string{
					"SELECT", "INSERT", "UPDATE", "DELETE", "TRUNCATE", "REFERENCES", "TRIGGER",
				},
				Pos: mml.Position{File: "grants.sql", Line: 3, Col: 1},
			}},
		},
		"REVOKE takes privileges back": {
			sql: "GRANT INSERT, DELETE ON orders TO bot;\n" +
				"REVOKE DELETE ON orders FROM bot;",
			table: "orders",
			want: []mml.Grant{{
				Role: "bot", Privileges: []string{"INSERT"},
				Pos: mml.Position{File: "grants.sql", Line: 3, Col: 1},
			}},
		},
		"a grant on a schema is not a table's": {
			sql:   "GRANT USAGE ON SCHEMA sales TO bot;",
			table: "refunds",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			src := postgres.Load(
				[]postgres.File{{Path: "grants.sql", Src: []byte(ddl + tc.sql)}},
				nil,
			)
			equals(t, tc.want, entity(t, src, tc.table).Grants)
		})
	}
}

func TestLoadMultiColumnChecks(t *testing.T) {
	t.Parallel()
	ddl := "CREATE TABLE samples (\n" +
		"  is_synthetic BOOLEAN NOT NULL,\n  generator_version TEXT,\n" +
		"  CHECK (NOT is_synthetic OR generator_version IS NOT NULL)\n);\n"
	cases := map[string]struct {
		after string
		want  []mml.Check
	}{
		"columns of the expression": {
			want: []mml.Check{{Columns: []string{"is_synthetic", "generator_version"}}},
		},
		"a rename follows the column": {
			after: "ALTER TABLE samples RENAME COLUMN generator_version TO generator;",
			want:  []mml.Check{{Columns: []string{"is_synthetic", "generator"}}},
		},
		"two columns compared": {
			after: "ALTER TABLE samples ADD COLUMN valid_from DATE, ADD COLUMN valid_to DATE," +
				" ADD CHECK (valid_to > valid_from);",
			want: []mml.Check{
				{Columns: []string{"is_synthetic", "generator_version"}},
				{Columns: []string{"valid_to", "valid_from"}},
			},
		},
		"dropping a column drops the check": {
			after: "ALTER TABLE samples DROP COLUMN generator_version;",
			want:  []mml.Check{},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			src := postgres.Load([]postgres.File{{Path: "s.sql", Src: []byte(ddl + tc.after)}}, nil)
			equals(t, tc.want, entity(t, src, "samples").Checks)
		})
	}
}

func TestLoadOnDelete(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		ref  string
		want string
	}{
		"unspecified is no action": {"REFERENCES nodes (node_id)", "no action"},
		"cascade":                  {"REFERENCES nodes (node_id) ON DELETE CASCADE", "cascade"},
		"set null":                 {"REFERENCES nodes (node_id) ON DELETE SET NULL", "set null"},
		"restrict":                 {"REFERENCES nodes (node_id) ON DELETE RESTRICT", "restrict"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ddl := "CREATE TABLE nodes (node_id BIGINT PRIMARY KEY);\n" +
				"CREATE TABLE edges (from_node_id BIGINT " + tc.ref + ");\n"
			src := postgres.Load([]postgres.File{{Path: "g.sql", Src: []byte(ddl)}}, nil)
			equals(t, tc.want, entity(t, src, "edges").Refs[0].OnDelete)
		})
	}
}

func TestLoadIndexesAndChanges(t *testing.T) {
	t.Parallel()
	ddl := "CREATE TABLE vendor_orders (id BIGINT PRIMARY KEY, erp_id TEXT, total NUMERIC);\n"
	at := func(line int) mml.Position {
		return mml.Position{File: "changes.sql", Line: line, Col: 1}
	}
	cases := map[string]struct {
		sql         string
		wantIndexes [][]string
		wantChanges []mml.Change
	}{
		"CREATE TABLE alone changes nothing": {},
		"an index lists its columns": {
			sql:         "CREATE UNIQUE INDEX ON vendor_orders (erp_id, total);",
			wantIndexes: [][]string{{"erp_id", "total"}},
			wantChanges: []mml.Change{{Statement: "CREATE INDEX", Pos: at(2)}},
		},
		"an expression ends the column list": {
			sql:         "CREATE INDEX ON vendor_orders (erp_id, lower(total::text));",
			wantIndexes: [][]string{{"erp_id"}},
			wantChanges: []mml.Change{{Statement: "CREATE INDEX", Pos: at(2)}},
		},
		"a renamed column renames its index": {
			sql: "CREATE INDEX ON vendor_orders (erp_id);\n" +
				"ALTER TABLE vendor_orders RENAME COLUMN erp_id TO source_id;",
			wantIndexes: [][]string{{"source_id"}},
			wantChanges: []mml.Change{
				{Statement: "CREATE INDEX", Pos: at(2)},
				{Statement: "RENAME", Pos: at(3)},
			},
		},
		"a dropped column drops its index": {
			sql: "CREATE INDEX ON vendor_orders (erp_id);\n" +
				"ALTER TABLE vendor_orders DROP COLUMN erp_id;",
			wantIndexes: [][]string{},
			wantChanges: []mml.Change{
				{Statement: "CREATE INDEX", Pos: at(2)},
				{Statement: "ALTER TABLE", Pos: at(3)},
			},
		},
		"a trigger": {
			sql: "CREATE TRIGGER audit AFTER UPDATE ON vendor_orders " +
				"FOR EACH ROW EXECUTE FUNCTION audit();",
			wantChanges: []mml.Change{{Statement: "CREATE TRIGGER", Pos: at(2)}},
		},
		"another table's statements are not this one's": {
			sql: "CREATE TABLE notes (id BIGINT PRIMARY KEY);\n" +
				"CREATE INDEX ON notes (id);",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			src := postgres.Load(
				[]postgres.File{{Path: "changes.sql", Src: []byte(ddl + tc.sql)}},
				nil,
			)
			e := entity(t, src, "vendor_orders")
			equals(t, tc.wantIndexes, e.Indexes)
			equals(t, tc.wantChanges, e.Changes)
		})
	}
}
