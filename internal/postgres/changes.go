package postgres

import (
	"strings"

	pg "github.com/pganalyze/pg_query_go/v6"

	mml "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// Statements recorded as an entity's Changes.
const (
	changeAlter   = "ALTER TABLE"
	changeRename  = "RENAME"
	changeIndex   = "CREATE INDEX"
	changeTrigger = "CREATE TRIGGER"
)

// loadAttached loads a statement that attaches something to an existing
// table: a grant, an index or a trigger. It reports false for any other
// statement.
func (l *loader) loadAttached(ctx *stmtCtx, n *pg.Node) bool {
	switch {
	case n.GetGrantStmt() != nil:
		l.grant(ctx, n.GetGrantStmt())
	case n.GetIndexStmt() != nil:
		l.createIndex(ctx, n.GetIndexStmt())
	case n.GetCreateTrigStmt() != nil:
		l.createTrigger(ctx, n.GetCreateTrigStmt())
	default:
		return false
	}
	return true
}

// recordChange notes that the statement being loaded modified e after its
// CREATE, so a design can tell a table the repo changes from one it only
// describes.
func recordChange(ctx *stmtCtx, e *mml.Entity, statement string) {
	e.Changes = append(e.Changes, mml.Change{Statement: statement, Pos: ctx.pos(ctx.start)})
}

// createIndex records an index's columns on its table. An expression element
// ends the list, because only the columns that lead an index decide which
// lookups it serves.
func (l *loader) createIndex(ctx *stmtCtx, is *pg.IndexStmt) {
	e, ok := l.find(strings.Split(tableKey(is.GetRelation()), "."))
	if !ok {
		return
	}
	var cols []string
	for _, p := range is.GetIndexParams() {
		name := p.GetIndexElem().GetName()
		if name == "" {
			break
		}
		cols = append(cols, name)
	}
	if len(cols) > 0 {
		e.Indexes = append(e.Indexes, cols)
	}
	recordChange(ctx, e, changeIndex)
}

// createTrigger records a trigger on its table. The linter reads nothing
// else from it: what the trigger does is in a function body it does not parse.
func (l *loader) createTrigger(ctx *stmtCtx, ts *pg.CreateTrigStmt) {
	if e, ok := l.find(strings.Split(tableKey(ts.GetRelation()), ".")); ok {
		recordChange(ctx, e, changeTrigger)
	}
}
