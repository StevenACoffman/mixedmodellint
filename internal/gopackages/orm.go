package gopackages

import (
	"go/types"
	"strconv"
	"strings"

	"golang.org/x/tools/go/ssa"

	mml "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// namedSQL rewrites sqlx's :name parameters to PostgreSQL's $1, $2, ..., one
// per occurrence in order, as sqlx does before it sends the query, and
// returns the names in that order. A :: cast and anything inside single
// quotes are left alone.
func namedSQL(sql string) (string, []string) {
	var b strings.Builder
	var names []string
	inQuote := false
	for i := 0; i < len(sql); i++ {
		ch := sql[i]
		if ch == '\'' {
			inQuote = !inQuote
		}
		if inQuote || ch != ':' {
			b.WriteByte(ch)
			continue
		}
		name, next := namedParam(sql, i)
		switch {
		case name != "":
			names = append(names, name)
			b.WriteString("$" + strconv.Itoa(len(names)))
		default:
			b.WriteString(sql[i:next])
		}
		i = next - 1
	}
	return b.String(), names
}

// namedParam reads the parameter at the colon sql[i]: its name and the
// index after it. A :: cast and a colon followed by no name have no name,
// and next skips past what was read.
func namedParam(sql string, i int) (name string, next int) {
	if i+1 < len(sql) && sql[i+1] == ':' {
		return "", i + 2
	}
	end := i + 1
	for end < len(sql) && identByte(sql[end]) {
		end++
	}
	return sql[i+1 : end], end
}

func identByte(c byte) bool {
	return c == '_' || c == '.' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' ||
		c >= 'A' && c <= 'Z'
}

// namedArgs returns, for each name, the origins of the values stored into
// the field of the struct arg that sqlx binds to it: the field whose db tag,
// or snake-cased name, is the name. A map or anything else leaves the names
// unknown.
func (t *tracer) namedArgs(arg ssa.Value, names []string) [][]mml.Origin {
	n := namedOf(unboxed(arg).Type())
	if n == nil {
		return nil
	}
	st, ok := n.Underlying().(*types.Struct)
	if !ok {
		return nil
	}
	out := make([][]mml.Origin, len(names))
	for i, name := range names {
		for j := range st.NumFields() {
			if columnName(st, j) == name {
				out[i] = t.storedOrigins(n, j)
			}
		}
	}
	return out
}

// storedOrigins returns the origins of every value the module stores into
// field j of the struct n: what a model or argument struct handed to an ORM
// carries into the write.
func (t *tracer) storedOrigins(n *types.Named, j int) []mml.Origin {
	var out []mml.Origin
	for _, v := range t.stores[fieldKey{n.Obj(), j}] {
		out = append(out, t.origins(v, 1, map[ssa.Value]bool{})...)
	}
	return out
}

// modelWrites returns a write for each call of gorm's Create or Save with a
// persisted struct: an INSERT (Save, which upserts on the primary key, is
// keyed) of every stored field, each with the origins of the values the
// module stores into that field. gorm builds the SQL at run time, so there
// is no snippet to parse; the write is built here instead.
func (t *tracer) modelWrites(fns []*ssa.Function) []mml.Query {
	var out []mml.Query
	for _, fn := range fns {
		for _, b := range fn.Blocks {
			for _, instr := range b.Instrs {
				call, ok := instr.(ssa.CallInstruction)
				if !ok {
					continue
				}
				if q, ok := t.modelWrite(call.Common()); ok {
					out = append(out, q)
				}
			}
		}
	}
	return out
}

func (t *tracer) modelWrite(c *ssa.CallCommon) (mml.Query, bool) {
	method, n, ok := gormModelCall(c)
	if !ok || !t.persisted[n.Obj()] {
		return mml.Query{}, false
	}
	at := t.rel(t.fset.Position(c.Pos()))
	q := mml.Query{Pos: at, Kind: mml.QueryInsert, Target: n.Obj().Name()}
	if method == "Save" {
		q.ConflictKey = []string{} // upsert on the primary key: keyed, columns not named here
	}
	st, _ := n.Underlying().(*types.Struct)
	for j := range st.NumFields() {
		f := st.Field(j)
		if f.Embedded() || !f.Exported() || columnName(st, j) == "-" {
			continue
		}
		q.Assignments = append(q.Assignments, mml.Assignment{
			Column: columnName(st, j), Origins: t.storedOrigins(n, j), Pos: at,
		})
	}
	return q, true
}

// gormModelCall reports whether c is gorm's Create or Save, and returns the
// method and the named type of the model passed to it.
func gormModelCall(c *ssa.CallCommon) (method string, model *types.Named, ok bool) {
	fn, args := callee(c)
	if fn == nil || len(args) == 0 || (fn.Name() != "Create" && fn.Name() != "Save") {
		return "", nil, false
	}
	recv := namedOf(fn.Type().(*types.Signature).Recv().Type())
	if recv == nil || recv.Obj().Pkg() == nil || recv.Obj().Pkg().Path() != "gorm.io/gorm" {
		return "", nil, false
	}
	n := namedOf(unboxed(args[0]).Type())
	return fn.Name(), n, n != nil
}

// unboxed returns the value inside any interface conversions.
func unboxed(v ssa.Value) ssa.Value {
	for {
		mi, ok := v.(*ssa.MakeInterface)
		if !ok {
			return v
		}
		v = mi.X
	}
}
