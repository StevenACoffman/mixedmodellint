// Package postgres loads PostgreSQL DDL and queries into the mixedmodellint
// IR. It wraps github.com/wasilibs/go-pgquery, the PostgreSQL parser compiled
// to WebAssembly, so the binary builds without cgo.
//
// Migrations are replayed in the order given: a CREATE TABLE in one file and
// an ALTER TABLE in a later one produce one entity with both applied.
package postgres

import (
	"strings"

	pg "github.com/pganalyze/pg_query_go/v6"
	pgquery "github.com/wasilibs/go-pgquery"

	mml "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// File is one SQL file's path and contents.
type File struct {
	Path string
	Src  []byte
}

// loader accumulates tables across files as migrations are replayed.
type loader struct {
	order  []string // table keys in creation order
	tables map[string]*mml.Entity
	out    mml.Source
	// named indexes, by sqlc query name, the writing Query each named
	// statement in a .sql file produced (see mergeNamed).
	named map[string]int
}

// stmtCtx is one statement being loaded: its file, the file's line index, and
// the statement's byte offset, which parse-tree locations are relative to the
// file rather than the statement.
type stmtCtx struct {
	file  *File
	lines lineIndex
	start int
	// directives are the "-- mml:" comments directly above the statement.
	directives mml.Annotations
	// at, when set, is where every position in the statement points: the Go
	// call site of a snippet, since offsets inside SQL folded from Go
	// constants do not map back to one place in the Go source.
	at *mml.Position
	// args are the origins of the Go values bound to $1, $2, ... of a
	// snippet. Origins keep their own Go positions, unlike everything at.
	args [][]mml.Origin
}

// Snippet is SQL found in Go code, at the call site Pos. Strict is true when
// it was passed to a known SQL API, so SQL that does not parse is reported;
// otherwise the argument may not be SQL at all and a parse failure is ignored.
// Args holds, for each bind parameter $n at index n-1, the origins of the Go
// value passed for it.
type Snippet struct {
	SQL    string
	Pos    mml.Position
	Strict bool
	Args   [][]mml.Origin
}

// Load parses every file in order, then every snippet of SQL found in Go, and
// returns the entities left after all DDL has been applied, the query corpus,
// and a Problem for each file or strict snippet that did not parse.
//
// Requires: files are in migration order (the shell sorts them by path).
// Ensures:  a file that fails to parse contributes a Problem and nothing else;
// the other files are still loaded.
func Load(files []File, snippets []Snippet) mml.Source {
	l := &loader{tables: map[string]*mml.Entity{}, named: map[string]int{}}
	for i := range files {
		l.loadFile(&files[i])
	}
	for i := range snippets {
		l.loadSnippet(&snippets[i])
	}
	for _, key := range l.order {
		if e, ok := l.tables[key]; ok {
			l.out.Entities = append(l.out.Entities, *e)
		}
	}
	return l.out
}

// loadSnippet parses SQL found in Go. Its statements are loaded like a
// file's, with every position at the call site.
func (l *loader) loadSnippet(sn *Snippet) {
	tree, err := pgquery.Parse(sn.SQL)
	if err != nil {
		if sn.Strict {
			l.out.Problems = append(l.out.Problems, mml.Problem{
				Message: "SQL passed from Go does not parse: " + err.Error(), Pos: sn.Pos,
			})
		}
		return
	}
	f := &File{Path: sn.Pos.File, Src: []byte(sn.SQL)}
	for _, raw := range tree.GetStmts() {
		ctx := &stmtCtx{
			file:       f,
			start:      int(raw.GetStmtLocation()),
			directives: mml.Annotations{},
			at:         &sn.Pos,
			args:       sn.Args,
		}
		before := len(l.out.Queries)
		l.loadStmt(ctx, raw.GetStmt())
		l.mergeNamed(sqlcName(stmtText(sn.SQL, raw)), before)
	}
}

// mergeNamed folds a snippet's statement into the .sql file statement sqlc
// generated it from, when both carry the same "-- name:" header. sqlc keeps
// queries in .sql files and copies each into a Go constant, so without this
// every query would be read twice and every finding about it reported twice.
// The file's Query stays, positioned in the file; the snippet adds the
// origins of the Go values bound to its parameters. before is the number of
// queries before the snippet's statement was loaded.
func (l *loader) mergeNamed(name string, before int) {
	i, ok := l.named[name]
	if !ok || len(l.out.Queries) == before {
		return
	}
	fileQ, goQ := &l.out.Queries[i], l.out.Queries[len(l.out.Queries)-1]
	if fileQ.Kind != goQ.Kind || !strings.EqualFold(fileQ.Target, goQ.Target) {
		return // the same name for a different statement: keep both
	}
	for _, ga := range goQ.Assignments {
		for j := range fileQ.Assignments {
			if strings.EqualFold(fileQ.Assignments[j].Column, ga.Column) {
				fileQ.Assignments[j].Origins = append(fileQ.Assignments[j].Origins, ga.Origins...)
			}
		}
	}
	l.out.Queries = l.out.Queries[:before]
}

// sqlcName returns the query name in a "-- name: CreateOrder :exec" comment
// that leads a statement, or "" when the statement has none.
func sqlcName(text string) string {
	for line := range strings.Lines(text) {
		line = strings.TrimSpace(line)
		rest, ok := strings.CutPrefix(line, "-- name:")
		switch {
		case ok:
			if f := strings.Fields(rest); len(f) > 0 {
				return f[0]
			}
			return ""
		case line == "" || strings.HasPrefix(line, "--"):
		default:
			return ""
		}
	}
	return ""
}

// stmtText is the source of one parsed statement, leading comments included.
func stmtText(src string, raw *pg.RawStmt) string {
	start, n := int(raw.GetStmtLocation()), int(raw.GetStmtLen())
	if n == 0 || start+n > len(src) {
		return src[start:]
	}
	return src[start : start+n]
}

func (l *loader) loadFile(f *File) {
	lines := newLineIndex(f.Src)
	tree, err := pgquery.Parse(string(f.Src))
	if err != nil {
		l.out.Problems = append(l.out.Problems, mml.Problem{
			Message: "SQL does not parse: " + err.Error(),
			Pos:     mml.Position{File: f.Path, Line: 1},
		})
		return
	}
	for _, raw := range tree.GetStmts() {
		start, anns := leadingDirectives(
			f,
			lines,
			int(raw.GetStmtLocation()),
			int(raw.GetStmtLen()),
		)
		ctx := &stmtCtx{file: f, lines: lines, start: start, directives: anns}
		before := len(l.out.Queries)
		l.loadStmt(ctx, raw.GetStmt())
		if name := sqlcName(
			stmtText(string(f.Src), raw),
		); name != "" &&
			len(l.out.Queries) > before {
			l.named[name] = len(l.out.Queries) - 1
		}
	}
}

func (l *loader) loadStmt(ctx *stmtCtx, n *pg.Node) {
	switch {
	case n.GetCreateStmt() != nil:
		l.createTable(ctx, n.GetCreateStmt())
	case n.GetAlterTableStmt() != nil:
		l.alterTable(ctx, n.GetAlterTableStmt())
	case n.GetCommentStmt() != nil:
		l.comment(ctx, n.GetCommentStmt())
	case n.GetDropStmt() != nil:
		l.drop(n.GetDropStmt())
	case n.GetViewStmt() != nil:
		l.createView(ctx, n.GetViewStmt())
	default:
		l.out.Queries = append(l.out.Queries, buildQueries(ctx, n)...)
	}
}

func (l *loader) createTable(ctx *stmtCtx, cs *pg.CreateStmt) {
	key := tableKey(cs.GetRelation())
	e := &mml.Entity{
		ID: "sql:" + key, Kind: mml.KindTable, Source: mml.SourceSQL,
		Name:        cs.GetRelation().GetRelname(),
		Pos:         ctx.pos(int(cs.GetRelation().GetLocation())),
		Annotations: mml.Annotations{},
	}
	for k, v := range ctx.directives {
		e.Annotations[k] = v
	}
	for _, elt := range cs.GetTableElts() {
		switch {
		case elt.GetColumnDef() != nil:
			addColumn(ctx, e, elt.GetColumnDef())
		case elt.GetConstraint() != nil:
			addConstraint(e, elt.GetConstraint(), "")
		}
	}
	if _, exists := l.tables[key]; !exists {
		l.order = append(l.order, key)
	}
	l.tables[key] = e
}

func (l *loader) createView(ctx *stmtCtx, vs *pg.ViewStmt) {
	key := tableKey(vs.GetView())
	e := &mml.Entity{
		ID: "sql:" + key, Kind: mml.KindView, Source: mml.SourceSQL,
		Name:        vs.GetView().GetRelname(),
		Pos:         ctx.pos(int(vs.GetView().GetLocation())),
		Annotations: mml.Annotations{},
	}
	for k, v := range ctx.directives {
		e.Annotations[k] = v
	}
	if _, exists := l.tables[key]; !exists {
		l.order = append(l.order, key)
	}
	l.tables[key] = e
	qs := buildQueries(ctx, vs.GetQuery())
	for i := range qs {
		if i == len(qs)-1 {
			qs[i].Kind, qs[i].Target = mml.QueryCreateView, e.Name
		}
	}
	l.out.Queries = append(l.out.Queries, qs...)
}

func (l *loader) alterTable(ctx *stmtCtx, as *pg.AlterTableStmt) {
	e, ok := l.find(strings.Split(tableKey(as.GetRelation()), "."))
	if !ok {
		return // altering a table defined outside the repo: nothing to update
	}
	for _, c := range as.GetCmds() {
		alterCmd(ctx, e, c.GetAlterTableCmd())
	}
}

// alterCmd applies one ALTER TABLE subcommand. Subcommands that do not change
// what the linter reads (ownership, storage, triggers) are ignored.
func alterCmd(ctx *stmtCtx, e *mml.Entity, cmd *pg.AlterTableCmd) {
	switch cmd.GetSubtype() {
	case pg.AlterTableType_AT_AddColumn:
		addColumn(ctx, e, cmd.GetDef().GetColumnDef())
	case pg.AlterTableType_AT_DropColumn:
		dropColumn(e, cmd.GetName())
	case pg.AlterTableType_AT_AddConstraint:
		addConstraint(e, cmd.GetDef().GetConstraint(), "")
	case pg.AlterTableType_AT_SetNotNull, pg.AlterTableType_AT_DropNotNull:
		if f, ok := e.Field(cmd.GetName()); ok {
			f.Nullable = cmd.GetSubtype() == pg.AlterTableType_AT_DropNotNull
		}
	case pg.AlterTableType_AT_AlterColumnType:
		if f, ok := e.Field(cmd.GetName()); ok {
			tn := cmd.GetDef().GetColumnDef().GetTypeName()
			f.Type, f.RawType = logicalType(tn), rawType(tn)
		}
	default:
	}
}

// comment applies COMMENT ON TABLE/VIEW/COLUMN: the text becomes the doc, and
// its "@key: value" lines become annotations positioned at the statement.
func (l *loader) comment(ctx *stmtCtx, cs *pg.CommentStmt) {
	names := stringList(cs.GetObject())
	pos := ctx.pos(ctx.start)
	anns := mml.ParseLines(cs.GetComment(), pos)
	switch cs.GetObjtype() {
	case pg.ObjectType_OBJECT_TABLE, pg.ObjectType_OBJECT_VIEW:
		if e, ok := l.find(names); ok {
			e.Doc = cs.GetComment()
			mergeAnnotations(e.Annotations, anns)
		}
	case pg.ObjectType_OBJECT_COLUMN:
		if len(names) < 2 {
			return
		}
		e, ok := l.find(names[:len(names)-1])
		if !ok {
			return
		}
		if f, ok := e.Field(names[len(names)-1]); ok {
			f.Doc = cs.GetComment()
			if f.Annotations == nil {
				f.Annotations = mml.Annotations{}
			}
			mergeAnnotations(f.Annotations, anns)
		}
	default:
	}
}

func (l *loader) drop(ds *pg.DropStmt) {
	if ds.GetRemoveType() != pg.ObjectType_OBJECT_TABLE &&
		ds.GetRemoveType() != pg.ObjectType_OBJECT_VIEW {
		return
	}
	for _, obj := range ds.GetObjects() {
		if e, ok := l.find(stringList(obj)); ok {
			delete(l.tables, strings.TrimPrefix(e.ID, "sql:"))
		}
	}
}

// find returns the table a possibly qualified name refers to: the exact key,
// or else the only table with that unqualified name, so "public.orders" finds
// a table created as "orders" (the default search_path).
func (l *loader) find(names []string) (*mml.Entity, bool) {
	if e, ok := l.tables[strings.Join(names, ".")]; ok {
		return e, true
	}
	if len(names) == 0 {
		return nil, false
	}
	var found *mml.Entity
	for _, e := range l.tables {
		if strings.EqualFold(e.Name, names[len(names)-1]) {
			if found != nil {
				return nil, false // ambiguous across schemas
			}
			found = e
		}
	}
	return found, found != nil
}

func mergeAnnotations(dst, src mml.Annotations) {
	for k, v := range src {
		dst[k] = v
	}
}

// tableKey is how a table is looked up across statements: the name as
// written, schema-qualified when the statement qualified it. "public.orders"
// and "orders" are different keys, as they would be under a search_path
// that does not start with public.
func tableKey(rv *pg.RangeVar) string {
	if rv.GetSchemaname() != "" {
		return rv.GetSchemaname() + "." + rv.GetRelname()
	}
	return rv.GetRelname()
}

func (c *stmtCtx) pos(offset int) mml.Position {
	if c.at != nil {
		return *c.at
	}
	if offset < 0 {
		offset = c.start
	}
	line, col := c.lines.at(offset)
	return mml.Position{File: c.file.Path, Line: line, Col: col}
}

// stringList returns the String values of a List node, or of a single String
// node: the dotted parts of a qualified name in COMMENT ON and DROP.
func stringList(n *pg.Node) []string {
	if s := n.GetString_(); s != nil {
		return []string{s.GetSval()}
	}
	var out []string
	for _, item := range n.GetList().GetItems() {
		out = append(out, item.GetString_().GetSval())
	}
	return out
}
