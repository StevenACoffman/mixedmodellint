package gopackages

import (
	"go/constant"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"

	mml "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// maxVariants caps how many strings one SQL argument may fold to, so a query
// built across many branches cannot explode the corpus.
const maxVariants = 8

// sink is a family of SQL-executing methods: the methods named in methods on
// the named types listed, in package pkg.
type sink struct {
	pkg     string
	types   []string
	methods []string
}

// databaseSQLMethods are the database/sql methods that take SQL text. A
// method of another interface with one of these names, such as an sqlc DBTX,
// is treated as a likely sink.
func databaseSQLMethods() []string {
	return []string{
		"Exec", "ExecContext", "Query", "QueryContext", "QueryRow", "QueryRowContext",
		"Prepare", "PrepareContext",
	}
}

// knownSinks are the SQL APIs whose string arguments are SQL. sqlx's embedded
// *sql.DB methods resolve to database/sql's own, so only sqlx's additions are
// listed for it.
func knownSinks() []sink {
	pgx := []string{"Exec", "Query", "QueryRow", "Prepare", "SendBatch"}
	return []sink{
		{pkg: "database/sql", types: []string{"DB", "Tx", "Conn"}, methods: databaseSQLMethods()},
		{pkg: "github.com/jackc/pgx/v5", types: []string{"Conn", "Tx"}, methods: pgx},
		{
			pkg:     "github.com/jackc/pgx/v5/pgxpool",
			types:   []string{"Pool", "Conn", "Tx"},
			methods: pgx,
		},
		{pkg: "github.com/jackc/pgx/v4", types: []string{"Conn", "Tx"}, methods: pgx},
		{
			pkg:     "github.com/jackc/pgx/v4/pgxpool",
			types:   []string{"Pool", "Conn", "Tx"},
			methods: pgx,
		},
		{
			pkg: "github.com/jmoiron/sqlx", types: []string{"DB", "Tx", "Conn"},
			methods: []string{
				"Get", "GetContext", "Select", "SelectContext", "Queryx", "QueryxContext",
				"QueryRowx", "QueryRowxContext", "MustExec", "MustExecContext", "NamedExec",
				"NamedExecContext", "NamedQuery", "NamedQueryContext", "Preparex",
				"PreparexContext", "PrepareNamed", "PrepareNamedContext",
			},
		},
		{pkg: "gorm.io/gorm", types: []string{"DB"}, methods: []string{"Raw", "Exec"}},
	}
}

// snippets returns the SQL passed to SQL APIs by the functions of pkgs, one
// snippet per string the argument can fold to.
func snippets(
	prog *ssa.Program,
	pkgs []*ssa.Package,
	rel func(token.Position) mml.Position,
) []Snippet {
	own := map[*ssa.Package]bool{}
	for _, p := range pkgs {
		if p != nil {
			own[p] = true
		}
	}
	sinks := knownSinks()
	var out []Snippet
	for _, fn := range sortedFunctions(prog, own) {
		for _, b := range fn.Blocks {
			for _, instr := range b.Instrs {
				call, ok := instr.(ssa.CallInstruction)
				if !ok {
					continue
				}
				out = append(out, callSnippets(prog, call.Common(), sinks, rel)...)
			}
		}
	}
	return out
}

// sortedFunctions returns the built functions of the packages in own, ordered
// by position, so the corpus is the same from run to run.
func sortedFunctions(prog *ssa.Program, own map[*ssa.Package]bool) []*ssa.Function {
	var fns []*ssa.Function
	for fn := range ssautil.AllFunctions(prog) {
		if fn.Pkg != nil && own[fn.Pkg] && len(fn.Blocks) > 0 {
			fns = append(fns, fn)
		}
	}
	slices.SortFunc(fns, func(a, b *ssa.Function) int { return int(a.Pos()) - int(b.Pos()) })
	return fns
}

// callSnippets returns the SQL a call passes, when it calls a sink.
func callSnippets(
	prog *ssa.Program, c *ssa.CallCommon, sinks []sink, rel func(token.Position) mml.Position,
) []Snippet {
	method, args := callee(c)
	if method == nil {
		return nil
	}
	strict, ok := matchSink(method, sinks)
	if !ok {
		return nil
	}
	i := sqlParam(method.Type().(*types.Signature))
	if i < 0 || i >= len(args) {
		return nil
	}
	pos := rel(prog.Fset.Position(c.Pos()))
	var out []Snippet
	for _, s := range foldString(args[i], map[ssa.Value]bool{}) {
		out = append(out, Snippet{SQL: s, Pos: pos, Strict: strict})
	}
	return out
}

// callee returns the method a call invokes and its arguments without the
// receiver: the interface method for a dynamic call, the static method for a
// direct one. Calls of plain functions return nil.
func callee(c *ssa.CallCommon) (*types.Func, []ssa.Value) {
	if c.IsInvoke() {
		return c.Method, c.Args
	}
	fn := c.StaticCallee()
	if fn == nil || fn.Signature.Recv() == nil || len(c.Args) == 0 {
		return nil, nil
	}
	obj, _ := fn.Object().(*types.Func)
	return obj, c.Args[1:]
}

// matchSink reports whether method executes SQL. strict is true for a known
// API; a method of any other named interface whose name is a database/sql
// method name matches with strict false.
func matchSink(method *types.Func, sinks []sink) (strict, ok bool) {
	recv := method.Type().(*types.Signature).Recv()
	if recv == nil {
		return false, false
	}
	named := namedOf(recv.Type())
	if named == nil || named.Obj().Pkg() == nil {
		return false, false
	}
	pkg, typ := named.Obj().Pkg().Path(), named.Obj().Name()
	for _, s := range sinks {
		if s.pkg == pkg && slices.Contains(s.types, typ) &&
			slices.Contains(s.methods, method.Name()) {
			return true, true
		}
	}
	_, isInterface := named.Underlying().(*types.Interface)
	return false, isInterface && slices.Contains(databaseSQLMethods(), method.Name())
}

// sqlParam returns the index of the parameter that carries SQL: the first
// string parameter named sql, query or statement, or else the first string
// parameter. pgx's Prepare(ctx, name, sql) is why the name is checked first.
func sqlParam(sig *types.Signature) int {
	first := -1
	for i := range sig.Params().Len() {
		p := sig.Params().At(i)
		if b, ok := p.Type().Underlying().(*types.Basic); !ok || b.Kind() != types.String {
			continue
		}
		switch p.Name() {
		case "sql", "query", "statement", "sqlStr":
			return i
		}
		if first < 0 {
			first = i
		}
	}
	return first
}

// foldString returns every string v can hold, when each is a compile-time
// constant: a constant, a + of foldable strings (each combination), a phi of
// foldable strings (each edge), or a conversion of one. It returns nil when
// any part is not constant, or when there would be more than maxVariants.
func foldString(v ssa.Value, visiting map[ssa.Value]bool) []string {
	if visiting[v] {
		return nil
	}
	visiting[v] = true
	defer delete(visiting, v)
	switch x := v.(type) {
	case *ssa.Const:
		if x.Value != nil && x.Value.Kind() == constant.String {
			return []string{constant.StringVal(x.Value)}
		}
	case *ssa.BinOp:
		if x.Op == token.ADD {
			return concat(foldString(x.X, visiting), foldString(x.Y, visiting))
		}
	case *ssa.Phi:
		return union(x.Edges, visiting)
	case *ssa.ChangeType:
		return foldString(x.X, visiting)
	case *ssa.Convert:
		return foldString(x.X, visiting)
	}
	return nil
}

func concat(left, right []string) []string {
	if len(left) == 0 || len(right) == 0 || len(left)*len(right) > maxVariants {
		return nil
	}
	out := make([]string, 0, len(left)*len(right))
	for _, l := range left {
		for _, r := range right {
			out = append(out, l+r)
		}
	}
	return out
}

func union(edges []ssa.Value, visiting map[ssa.Value]bool) []string {
	var out []string
	for _, e := range edges {
		vs := foldString(e, visiting)
		if len(vs) == 0 {
			return nil
		}
		for _, s := range vs {
			if !slices.Contains(out, s) {
				out = append(out, s)
			}
		}
		if len(out) > maxVariants {
			return nil
		}
	}
	return out
}
