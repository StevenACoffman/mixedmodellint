package gopackages

import (
	"go/constant"
	"go/token"
	"go/types"
	"reflect"
	"strings"
	"time"

	"golang.org/x/tools/go/ssa"

	mml "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// maxDepth bounds how many struct fields the tracer follows through, which is
// enough for a caller filling an sqlc Params struct and short enough that a
// value passed around a large program is given up on rather than chased.
const maxDepth = 4

// fieldKey names one field of a named struct type.
type fieldKey struct {
	typ   *types.TypeName
	index int
}

// tracer finds where Go values bound to SQL parameters come from. stores
// indexes every value the module stores into a field of a named struct, so
// a value read from an sqlc Params struct can be followed back to the caller
// that filled it; callers indexes the static calls of each function, so a
// wrapper's parameters can be followed to the values its callers pass.
type tracer struct {
	fset      *token.FileSet
	rel       func(token.Position) mml.Position
	persisted map[*types.TypeName]bool
	stores    map[fieldKey][]ssa.Value
	callers   map[*ssa.Function][]*ssa.CallCommon
}

func newTracer(
	prog *ssa.Program, fns []*ssa.Function, persisted map[*types.TypeName]bool,
	rel func(token.Position) mml.Position,
) *tracer {
	t := &tracer{
		fset:      prog.Fset,
		rel:       rel,
		persisted: persisted,
		stores:    map[fieldKey][]ssa.Value{},
		callers:   map[*ssa.Function][]*ssa.CallCommon{},
	}
	for _, fn := range fns {
		for _, b := range fn.Blocks {
			for _, instr := range b.Instrs {
				t.index(instr)
			}
		}
	}
	return t
}

// index records what one instruction stores into a struct field, or which
// function it calls.
func (t *tracer) index(instr ssa.Instruction) {
	if k, v, ok := fieldStore(instr); ok {
		t.stores[k] = append(t.stores[k], v)
	}
	if call, ok := instr.(ssa.CallInstruction); ok {
		if callee := call.Common().StaticCallee(); callee != nil {
			t.callers[callee] = append(t.callers[callee], call.Common())
		}
	}
}

// fieldStore returns the field and value of an instruction that stores into
// a field of a named struct.
func fieldStore(instr ssa.Instruction) (fieldKey, ssa.Value, bool) {
	st, ok := instr.(*ssa.Store)
	if !ok {
		return fieldKey{}, nil, false
	}
	fa, ok := st.Addr.(*ssa.FieldAddr)
	if !ok {
		return fieldKey{}, nil, false
	}
	n := namedOf(fa.X.Type())
	if n == nil {
		return fieldKey{}, nil, false
	}
	return fieldKey{n.Obj(), fa.Field}, st.Val, true
}

// args returns the origins of each value a call binds to $1, $2, ...: the
// elements of its variadic argument, when the SQL parameter comes just
// before it.
func (t *tracer) args(sig *types.Signature, sqlIndex int, args []ssa.Value) [][]mml.Origin {
	if !sig.Variadic() || sqlIndex != sig.Params().Len()-2 || len(args) != sig.Params().Len() {
		return nil
	}
	elems := variadicElems(args[len(args)-1])
	out := make([][]mml.Origin, len(elems))
	for i, v := range elems {
		if v != nil {
			out[i] = t.origins(v, 0, map[ssa.Value]bool{})
		}
	}
	return out
}

// variadicElems returns the values packed into a variadic argument, by
// index. SSA builds f(a, b) for f(...any) as a local array whose elements are
// stored one by one and then sliced; anything else, such as args... passed
// through, has no elements the tracer can see.
func variadicElems(v ssa.Value) []ssa.Value {
	sl, ok := v.(*ssa.Slice)
	if !ok {
		return nil
	}
	arr, ok := sl.X.(*ssa.Alloc)
	if !ok {
		return nil
	}
	at, ok := deref(arr.Type()).Underlying().(*types.Array)
	if !ok {
		return nil
	}
	out := make([]ssa.Value, at.Len())
	for _, ref := range *arr.Referrers() {
		ia, ok := ref.(*ssa.IndexAddr)
		if !ok {
			continue
		}
		if i, ok := constInt(ia.Index); ok && i >= 0 && i < int64(len(out)) {
			out[i] = storedAt(ia)
		}
	}
	return out
}

// storedAt returns the value stored at an element address, or nil.
func storedAt(addr *ssa.IndexAddr) ssa.Value {
	for _, r := range *addr.Referrers() {
		if st, ok := r.(*ssa.Store); ok && st.Addr == addr {
			return st.Val
		}
	}
	return nil
}

// origins returns how v arises.
//
// Requires: v belongs to the program the tracer indexed.
// Ensures:  a field read from a persisted struct is a copy of that field; a
// field read from any other struct has the origins of every value stored
// into that field; time.Time's Truncate and Round by a constant, Format, and
// a round trip through Unix or UnixMilli add a truncation to the origins of
// the time they start from. Anything else, a cycle, more than maxVariants
// origins or more than maxDepth fields returns nil: nothing is known.
func (t *tracer) origins(v ssa.Value, depth int, visiting map[ssa.Value]bool) []mml.Origin {
	if visiting[v] || depth > maxDepth {
		return nil
	}
	visiting[v] = true
	defer delete(visiting, v)
	var out []mml.Origin
	if inner, ok := sameValue(v); ok {
		out = t.origins(inner, depth, visiting)
	} else if typ, index, pos, ok := fieldRead(v); ok {
		out = t.field(typ, index, pos, depth, visiting)
	} else {
		switch x := v.(type) {
		case *ssa.Phi:
			for _, e := range x.Edges {
				out = append(out, t.origins(e, depth, visiting)...)
			}
		case *ssa.Call:
			out = t.call(x, depth, visiting)
		}
	}
	if len(out) > maxVariants {
		return nil
	}
	return out
}

// sameValue returns the value a conversion or boxing wraps: the stored value
// is the wrapped one.
func sameValue(v ssa.Value) (ssa.Value, bool) {
	switch x := v.(type) {
	case *ssa.ChangeType:
		return x.X, true
	case *ssa.Convert:
		return x.X, true
	case *ssa.MakeInterface:
		return x.X, true
	default:
		return nil, false
	}
}

// fieldRead returns the struct type, field index and position of a read of a
// struct field, through a value (s.F) or a pointer (p.F).
func fieldRead(v ssa.Value) (types.Type, int, token.Pos, bool) {
	switch x := v.(type) {
	case *ssa.Field:
		return x.X.Type(), x.Field, x.Pos(), true
	case *ssa.UnOp:
		if fa, ok := x.X.(*ssa.FieldAddr); ok && x.Op == token.MUL {
			return fa.X.Type(), fa.Field, fa.Pos(), true
		}
	}
	return nil, 0, token.NoPos, false
}

// field returns the origins of field index of the struct typ points to or
// is, read at pos.
func (t *tracer) field(
	typ types.Type, index int, pos token.Pos, depth int, visiting map[ssa.Value]bool,
) []mml.Origin {
	n := namedOf(typ)
	if n == nil {
		return nil
	}
	if t.persisted[n.Obj()] {
		st, _ := n.Underlying().(*types.Struct)
		return []mml.Origin{{
			From: mml.FieldRef{Entity: n.Obj().Name(), Field: columnName(st, index)},
			Pos:  t.rel(t.fset.Position(pos)),
		}}
	}
	var out []mml.Origin
	for _, v := range t.stores[fieldKey{n.Obj(), index}] {
		out = append(out, t.origins(v, depth+1, visiting)...)
	}
	return out
}

// call returns the origins of a time.Time produced by truncating, formatting
// or round-tripping another, or by changing only its location.
func (t *tracer) call(c *ssa.Call, depth int, visiting map[ssa.Value]bool) []mml.Origin {
	fn := c.Call.StaticCallee()
	if fn == nil || !inPackage(fn, "time") {
		return nil
	}
	unit, from := timeCall(fn.Name(), c.Call.Args)
	if from == nil {
		return nil
	}
	origins := t.origins(from, depth, visiting)
	if unit == "" {
		return origins
	}
	return truncated(origins, unit, t.rel(t.fset.Position(c.Pos())))
}

// timeCall returns, for a call of the time package's function or method
// name, the unit its result is truncated to and the time it starts from. A
// location change starts from its receiver and truncates nothing; a call the
// tracer does not know returns a nil from.
func timeCall(name string, args []ssa.Value) (unit string, from ssa.Value) {
	switch name {
	case "UTC", "Local", "In":
		return "", args[0]
	case "Truncate", "Round":
		if d, ok := constInt(args[1]); ok {
			if unit = durationUnit(time.Duration(d)); unit != "" {
				return unit, args[0]
			}
		}
	case "Format":
		if layout, ok := constString(args[1]); ok {
			if unit = layoutUnit(layout); unit != "" {
				return unit, args[0]
			}
		}
	case "Unix", "UnixMilli":
		return roundTrip(name, args)
	}
	return "", nil
}

// roundTrip recognizes time.Unix(t.Unix(), 0) and time.UnixMilli(t.UnixMilli()),
// which rebuild t without its sub-second or sub-millisecond part, and returns
// the unit kept and the t the round trip started from.
func roundTrip(name string, args []ssa.Value) (string, ssa.Value) {
	inner, ok := args[0].(*ssa.Call)
	if !ok {
		return "", nil
	}
	callee := inner.Call.StaticCallee()
	if callee == nil || !inPackage(callee, "time") || callee.Name() != name ||
		callee.Signature.Recv() == nil {
		return "", nil
	}
	switch name {
	case "Unix":
		if ns, ok := constInt(args[1]); !ok || ns != 0 {
			return "", nil
		}
		return "second", inner.Call.Args[0]
	default:
		return "millisecond", inner.Call.Args[0]
	}
}

// truncated marks origins, in place, as truncated to unit at pos; with no
// known origin, the truncation is still known.
func truncated(origins []mml.Origin, unit string, pos mml.Position) []mml.Origin {
	if len(origins) == 0 {
		return []mml.Origin{{Truncate: unit, Pos: pos}}
	}
	for i := range origins {
		origins[i].Truncate, origins[i].Pos = unit, pos
	}
	return origins
}

// durationUnit is the coarsest unit a constant duration covers: Truncate(90 *
// time.Second) keeps minutes and coarser, so it truncates to the minute.
// Durations under a microsecond keep everything PostgreSQL stores.
func durationUnit(d time.Duration) string {
	switch {
	case d >= 24*time.Hour:
		return "day"
	case d >= time.Hour:
		return "hour"
	case d >= time.Minute:
		return "minute"
	case d >= time.Second:
		return "second"
	case d >= time.Millisecond:
		return "millisecond"
	case d >= time.Microsecond:
		return "microsecond"
	default:
		return ""
	}
}

// layoutUnit is the finest unit a time.Format layout writes: none when it
// has fractional seconds (".000", ".999999999"), else seconds, minutes, hours
// or the day. time.RFC3339 drops fractional seconds; time.RFC3339Nano keeps
// them.
func layoutUnit(layout string) string {
	switch {
	case strings.Contains(layout, ".0") || strings.Contains(layout, ".9") ||
		strings.Contains(layout, ",0") || strings.Contains(layout, ",9"):
		return ""
	case strings.Contains(layout, "05"):
		return "second"
	case strings.Contains(layout, "04"):
		return "minute"
	case strings.Contains(layout, "15") || strings.Contains(layout, "03"):
		return "hour"
	default:
		return "day"
	}
}

// columnName is the stored name of field index of st, as entities reads it.
func columnName(st *types.Struct, index int) string {
	if st == nil || index >= st.NumFields() {
		return ""
	}
	name, _, _ := tagName(reflect.StructTag(st.Tag(index)))
	if name == "" {
		name = snake(st.Field(index).Name())
	}
	return name
}

// inPackage reports whether fn is declared in the package with path pkg. It
// reads the types object, because a function of a package the program did
// not build from source has no ssa.Package.
func inPackage(fn *ssa.Function, pkg string) bool {
	obj := fn.Object()
	return obj != nil && obj.Pkg() != nil && obj.Pkg().Path() == pkg
}

func constInt(v ssa.Value) (int64, bool) {
	c, ok := v.(*ssa.Const)
	if !ok || c.Value == nil || c.Value.Kind() != constant.Int {
		return 0, false
	}
	return constant.Int64Val(c.Value)
}

func constString(v ssa.Value) (string, bool) {
	c, ok := v.(*ssa.Const)
	if !ok || c.Value == nil || c.Value.Kind() != constant.String {
		return "", false
	}
	return constant.StringVal(c.Value), true
}
