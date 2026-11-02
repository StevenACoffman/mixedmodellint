package postgres

import (
	"strings"

	pg "github.com/pganalyze/pg_query_go/v6"

	mml "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// queryBuilder reduces one statement to the core's query model. Each SELECT
// level (a CTE, a subquery in FROM, each side of a UNION) becomes its own
// Query; the enclosing level sees it as a derived relation.
type queryBuilder struct {
	ctx  *stmtCtx
	ctes map[string]bool
	// ones are the output names compared with the constant 1 anywhere in the
	// statement (WHERE rn = 1), which is how a ROW_NUMBER() dedup is filtered.
	ones map[string]bool
	out  []mml.Query
}

// buildQueries returns the queries of a statement that is not DDL. Statements
// the model has nothing to say about (SET, GRANT, ...) return nil.
func buildQueries(ctx *stmtCtx, n *pg.Node) []mml.Query {
	b := &queryBuilder{ctx: ctx, ctes: map[string]bool{}, ones: map[string]bool{}}
	walkAll(n, func(x *pg.Node) {
		if ae := x.GetAExpr(); ae != nil && opName(ae) == "=" {
			if c := columnName(ae.GetLexpr()); c != "" && isOne(ae.GetRexpr()) {
				b.ones[strings.ToLower(c)] = true
			}
		}
	})
	b.statement(n)
	return b.out
}

// statement adds the queries of one statement by its kind.
func (b *queryBuilder) statement(n *pg.Node) {
	switch {
	case n.GetSelectStmt() != nil:
		b.selectStmt(n.GetSelectStmt(), mml.QuerySelect, "")
	case n.GetInsertStmt() != nil:
		b.insert(n.GetInsertStmt())
	case n.GetUpdateStmt() != nil:
		b.update(n.GetUpdateStmt())
	case n.GetMergeStmt() != nil:
		b.merge(n.GetMergeStmt())
	case n.GetDeleteStmt() != nil:
		b.delete(n.GetDeleteStmt())
	case n.GetTruncateStmt() != nil:
		for _, r := range n.GetTruncateStmt().GetRelations() {
			b.out = append(b.out, mml.Query{
				Pos: b.ctx.pos(
					b.ctx.start,
				),
				Kind:   mml.QueryTruncate,
				Target: tableKey(r.GetRangeVar()),
			})
		}
	}
}

// selectStmt adds the queries of one SELECT. kind and target are set when the
// SELECT is the body of an INSERT or a view, so the last Query added is the
// writing one.
func (b *queryBuilder) selectStmt(s *pg.SelectStmt, kind mml.QueryKind, target string) {
	b.with(s.GetWithClause())
	if s.GetLarg() != nil || s.GetRarg() != nil {
		b.selectStmt(s.GetLarg(), kind, target)
		b.selectStmt(s.GetRarg(), kind, target)
		return
	}
	q := mml.Query{Pos: b.ctx.pos(b.ctx.start), Kind: kind, Target: target}
	for _, f := range s.GetFromClause() {
		b.fromItem(&q, f)
	}
	scope := defaultRelation(q.Relations)
	for _, t := range s.GetTargetList() {
		b.expr(&q, scope, t)
	}
	b.expr(&q, scope, s.GetHavingClause())
	b.expr(&q, scope, s.GetWhereClause())
	walk(s.GetWhereClause(), func(x *pg.Node) {
		if cr := x.GetColumnRef(); cr != nil {
			q.Filters = append(q.Filters, columnRef(cr, scope))
		}
	})
	for _, g := range s.GetGroupClause() {
		if cr := g.GetColumnRef(); cr != nil {
			q.GroupBy = append(q.GroupBy, columnRef(cr, scope))
		}
	}
	b.dedups(&q, s, scope)
	b.out = append(b.out, q)
}

// with adds the queries of a WITH clause's CTEs and records their names, so
// a FROM item naming one is derived rather than a stored table. INSERT and
// UPDATE carry their own WITH clause, outside the SELECT.
func (b *queryBuilder) with(wc *pg.WithClause) {
	for _, c := range wc.GetCtes() {
		cte := c.GetCommonTableExpr()
		b.ctes[strings.ToLower(cte.GetCtename())] = true
		if sub := cte.GetCtequery().GetSelectStmt(); sub != nil {
			b.selectStmt(sub, mml.QuerySelect, "")
		}
	}
}

// fromItem adds one FROM item: a table, a CTE or subquery (derived), or a
// join, whose two sides are added in order.
func (b *queryBuilder) fromItem(q *mml.Query, n *pg.Node) {
	switch {
	case n.GetRangeVar() != nil:
		rv := n.GetRangeVar()
		name := tableKey(rv)
		alias := rv.GetAlias().GetAliasname()
		if alias == "" {
			alias = rv.GetRelname()
		}
		q.Relations = append(q.Relations, mml.Relation{
			Name: name, Alias: alias, Derived: b.ctes[strings.ToLower(rv.GetRelname())],
		})
	case n.GetRangeSubselect() != nil:
		rs := n.GetRangeSubselect()
		if sub := rs.GetSubquery().GetSelectStmt(); sub != nil {
			b.selectStmt(sub, mml.QuerySelect, "")
		}
		alias := rs.GetAlias().GetAliasname()
		q.Relations = append(q.Relations, mml.Relation{Name: alias, Alias: alias, Derived: true})
	case n.GetJoinExpr() != nil:
		b.join(q, n.GetJoinExpr())
	}
}

func (b *queryBuilder) join(q *mml.Query, je *pg.JoinExpr) {
	b.fromItem(q, je.GetLarg())
	leftAlias := ""
	if len(q.Relations) > 0 {
		leftAlias = q.Relations[len(q.Relations)-1].Alias
	}
	b.fromItem(q, je.GetRarg())
	right := q.Relations[len(q.Relations)-1].Alias
	j := mml.Join{Right: right, Pos: b.ctx.pos(b.ctx.start)}
	for _, u := range je.GetUsingClause() {
		col := u.GetString_().GetSval()
		j.On = append(j.On, mml.ColumnPair{
			Left:  mml.ColumnRef{Relation: leftAlias, Column: col},
			Right: mml.ColumnRef{Relation: right, Column: col},
		})
	}
	for _, eq := range equalities(je.GetQuals()) {
		l, r := columnRef(eq[0], ""), columnRef(eq[1], "")
		if strings.EqualFold(l.Relation, right) {
			l, r = r, l
		}
		j.On = append(j.On, mml.ColumnPair{Left: l, Right: r})
	}
	q.Joins = append(q.Joins, j)
}

// equalities returns the column = column comparisons of a condition that is
// one equality or an AND of them.
func equalities(n *pg.Node) [][2]*pg.ColumnRef {
	if be := n.GetBoolExpr(); be != nil && be.GetBoolop() == pg.BoolExprType_AND_EXPR {
		var out [][2]*pg.ColumnRef
		for _, a := range be.GetArgs() {
			out = append(out, equalities(a)...)
		}
		return out
	}
	ae := n.GetAExpr()
	if ae == nil || opName(ae) != "=" {
		return nil
	}
	l, r := ae.GetLexpr().GetColumnRef(), ae.GetRexpr().GetColumnRef()
	if l == nil || r == nil {
		return nil
	}
	return [][2]*pg.ColumnRef{{l, r}}
}

// expr records the aggregates and time-unit expressions inside n.
func (b *queryBuilder) expr(q *mml.Query, scope string, n *pg.Node) {
	walk(n, func(x *pg.Node) {
		if fc := x.GetFuncCall(); fc != nil {
			b.funcCall(q, scope, fc)
		}
		if ae := x.GetAExpr(); ae != nil {
			if te, ok := timeOfDayCompare(ae, scope); ok {
				te.Pos = b.ctx.pos(int(ae.GetLocation()))
				q.TimeExprs = append(q.TimeExprs, te)
			}
		}
	})
}

func (b *queryBuilder) funcCall(q *mml.Query, scope string, fc *pg.FuncCall) {
	name := funcName(fc)
	pos := b.ctx.pos(int(fc.GetLocation()))
	args := fc.GetArgs()
	switch {
	case fc.GetOver() == nil && isAggregate(name):
		a := mml.Aggregate{
			Func:     name,
			Star:     fc.GetAggStar(),
			Distinct: fc.GetAggDistinct(),
			Pos:      pos,
		}
		if len(args) > 0 {
			if cr := unwrapCast(args[0]).GetColumnRef(); cr != nil {
				a.Arg = columnRef(cr, scope)
			}
		}
		q.Aggregates = append(q.Aggregates, a)
	case name == "date_trunc" || name == "date_part" || name == "extract":
		if len(args) == 2 {
			if cr := unwrapCast(args[1]).GetColumnRef(); cr != nil {
				q.TimeExprs = append(q.TimeExprs, mml.TimeExpr{
					Func: name, Unit: constString(args[0]), Column: columnRef(cr, scope), Pos: pos,
				})
			}
		}
	}
}

// dedups records the ways s removes duplicate rows: a filtered
// ROW_NUMBER()/RANK() window, DISTINCT ON, DISTINCT, or a GROUP BY whose
// aggregates only pick one value per group.
func (b *queryBuilder) dedups(q *mml.Query, s *pg.SelectStmt, scope string) {
	pos := b.ctx.pos(b.ctx.start)
	q.Dedups = append(q.Dedups, b.windowDedups(s, scope, pos)...)
	if d, ok := distinctDedup(s, scope, pos); ok {
		q.Dedups = append(q.Dedups, d)
	}
	if len(q.GroupBy) > 0 && onlyPickers(q.Aggregates) {
		q.Dedups = append(
			q.Dedups,
			mml.Dedup{Kind: mml.DedupGroupBy, Partition: q.GroupBy, Pos: pos},
		)
	}
}

// windowDedups returns the ranking windows in s's output whose result the
// statement compares with 1.
func (b *queryBuilder) windowDedups(s *pg.SelectStmt, scope string, pos mml.Position) []mml.Dedup {
	var out []mml.Dedup
	for _, t := range s.GetTargetList() {
		rt := t.GetResTarget()
		fc := rt.GetVal().GetFuncCall()
		if fc == nil || fc.GetOver() == nil || !b.ones[strings.ToLower(rt.GetName())] {
			continue
		}
		switch funcName(fc) {
		case "row_number", "rank", "dense_rank":
			out = append(out, windowDedup(fc.GetOver(), scope, pos))
		}
	}
	return out
}

// distinctDedup reads DISTINCT ON (cols), whose partition is cols, and plain
// DISTINCT, whose partition is every output column. The parser represents
// plain DISTINCT as a one-element list holding an empty node.
func distinctDedup(s *pg.SelectStmt, scope string, pos mml.Position) (mml.Dedup, bool) {
	dc := s.GetDistinctClause()
	if len(dc) == 0 {
		return mml.Dedup{}, false
	}
	if len(dc) > 1 || dc[0].GetNode() != nil {
		return mml.Dedup{
			Kind:      mml.DedupDistinctOn,
			Partition: columnRefs(dc, scope),
			Pos:       pos,
		}, true
	}
	d := mml.Dedup{Kind: mml.DedupDistinct, Pos: pos}
	for _, t := range s.GetTargetList() {
		if cr := t.GetResTarget().GetVal().GetColumnRef(); cr != nil {
			d.Partition = append(d.Partition, columnRef(cr, scope))
		}
	}
	return d, true
}

func windowDedup(w *pg.WindowDef, scope string, pos mml.Position) mml.Dedup {
	d := mml.Dedup{
		Kind:      mml.DedupWindow,
		Partition: columnRefs(w.GetPartitionClause(), scope),
		Pos:       pos,
	}
	for i, o := range w.GetOrderClause() {
		sb := o.GetSortBy()
		if cr := sb.GetNode().GetColumnRef(); cr != nil {
			d.OrderBy = append(d.OrderBy, columnRef(cr, scope))
		}
		if i == 0 {
			d.Descending = sb.GetSortbyDir() == pg.SortByDir_SORTBY_DESC
		}
	}
	return d
}

func onlyPickers(aggs []mml.Aggregate) bool {
	for _, a := range aggs {
		switch a.Func {
		case "min", "max", "any_value":
		default:
			return false
		}
	}
	return true
}

func (b *queryBuilder) insert(is *pg.InsertStmt) {
	target := tableKey(is.GetRelation())
	var key []string
	for _, ie := range is.GetOnConflictClause().GetInfer().GetIndexElems() {
		key = append(key, ie.GetIndexElem().GetName())
	}
	b.with(is.GetWithClause())
	before := len(b.out)
	sel := is.GetSelectStmt().GetSelectStmt()
	if sel != nil && len(sel.GetValuesLists()) == 0 {
		b.selectStmt(sel, mml.QueryInsert, target)
	}
	if len(b.out) == before {
		b.out = append(b.out, mml.Query{Kind: mml.QueryInsert, Target: target})
	} else {
		b.out[len(b.out)-1].FromSelect = true
	}
	w := &b.out[len(b.out)-1]
	w.Pos, w.ConflictKey = b.ctx.pos(b.ctx.start), key
	if is.GetOnConflictClause() != nil && key == nil {
		w.ConflictKey = []string{} // ON CONFLICT ON CONSTRAINT name: keyed, columns unknown
	}
	w.Assignments = b.insertAssignments(w, is, sel)
}

// insertAssignments returns the columns an INSERT sets. Without a column list
// the columns are the table's in DDL order, which the statement alone does
// not say, so there are none; a UNION body has several target lists and is
// skipped too.
func (b *queryBuilder) insertAssignments(
	w *mml.Query, is *pg.InsertStmt, sel *pg.SelectStmt,
) []mml.Assignment {
	cols := make([]string, 0, len(is.GetCols()))
	for _, c := range is.GetCols() {
		cols = append(cols, c.GetResTarget().GetName())
	}
	a := &assigner{args: b.ctx.args, pos: w.Pos}
	switch {
	case len(cols) == 0 || sel == nil:
		return nil
	case len(sel.GetValuesLists()) > 0:
		return a.valuesAssignments(cols, sel.GetValuesLists())
	case sel.GetLarg() != nil:
		return nil
	default:
		a.relations = w.Relations
		return a.selectAssignments(cols, sel.GetTargetList())
	}
}

// update adds an UPDATE. Its FROM items are only scope for the SET list: the
// statement reads them, but its Query is the write, so they are not added as
// its relations.
func (b *queryBuilder) update(us *pg.UpdateStmt) {
	target := us.GetRelation()
	alias := target.GetAlias().GetAliasname()
	if alias == "" {
		alias = target.GetRelname()
	}
	b.with(us.GetWithClause())
	scope := mml.Query{Relations: []mml.Relation{{Name: tableKey(target), Alias: alias}}}
	for _, f := range us.GetFromClause() {
		b.fromItem(&scope, f)
	}
	pos := b.ctx.pos(b.ctx.start)
	a := &assigner{relations: scope.Relations, args: b.ctx.args, pos: pos}
	b.out = append(b.out, mml.Query{
		Pos: pos, Kind: mml.QueryUpdate, Target: tableKey(target),
		Assignments: a.setAssignments(us.GetTargetList()),
	})
}

// delete adds a DELETE with the columns its WHERE clause tests, so a
// retention delete (WHERE event_time < ...) can be told from a delete by key.
func (b *queryBuilder) delete(ds *pg.DeleteStmt) {
	b.with(ds.GetWithClause())
	target := ds.GetRelation()
	alias := target.GetAlias().GetAliasname()
	if alias == "" {
		alias = target.GetRelname()
	}
	q := mml.Query{
		Pos: b.ctx.pos(b.ctx.start), Kind: mml.QueryDelete, Target: tableKey(target),
		Relations: []mml.Relation{{Name: tableKey(target), Alias: alias}},
	}
	walk(ds.GetWhereClause(), func(x *pg.Node) {
		if cr := x.GetColumnRef(); cr != nil {
			q.Filters = append(q.Filters, columnRef(cr, alias))
		}
	})
	b.out = append(b.out, q)
}

func (b *queryBuilder) merge(ms *pg.MergeStmt) {
	target := ms.GetRelation()
	alias := target.GetAlias().GetAliasname()
	if alias == "" {
		alias = target.GetRelname()
	}
	key := []string{}
	for _, eq := range equalities(ms.GetJoinCondition()) {
		for _, side := range eq {
			if c := columnRef(side, ""); strings.EqualFold(c.Relation, alias) {
				key = append(key, c.Column)
			}
		}
	}
	b.out = append(b.out, mml.Query{
		Pos: b.ctx.pos(
			b.ctx.start,
		),
		Kind:        mml.QueryMerge,
		Target:      tableKey(target),
		ConflictKey: key,
	})
}
