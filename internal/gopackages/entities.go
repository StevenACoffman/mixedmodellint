package gopackages

import (
	"go/ast"
	"go/token"
	"go/types"
	"reflect"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"

	mml "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// structDecl is one named struct type declared in a loaded package, with the
// doc comment its declaration carries.
type structDecl struct {
	obj *types.TypeName
	st  *types.Struct
	doc *ast.CommentGroup
	pkg *packages.Package
}

// goField is one persisted field with what its tags said about it.
type goField struct {
	field     mml.Field
	stored    bool
	pk        bool
	refTarget *types.TypeName
}

// entities returns one entity per persisted struct in pkgs. A struct is
// persisted when a field has a storage tag or an mml tag, or its doc comment
// has an //mml: directive. A struct that is only ever embedded (an audit
// mixin) is part of the structs that embed it, not an entity of its own,
// unless a directive says otherwise.
func entities(pkgs []*packages.Package, rel func(token.Position) mml.Position) []mml.Entity {
	decls := structDecls(pkgs)
	byObj := make(map[*types.TypeName]*structDecl, len(decls))
	for i := range decls {
		byObj[decls[i].obj] = &decls[i]
	}
	mixins := embeddedTypes(decls)
	var out []mml.Entity
	for i := range decls {
		d := &decls[i]
		anns := directives(d.doc, d.pkg.Fset, rel)
		if mixins[d.obj] && len(anns) == 0 {
			continue
		}
		if e, ok := toEntity(d, anns, byObj, rel); ok {
			out = append(out, e)
		}
	}
	return out
}

// structDecls finds every named struct type declared at package level, in
// source order.
func structDecls(pkgs []*packages.Package) []structDecl {
	var out []structDecl
	for _, p := range pkgs {
		if p.TypesInfo == nil {
			continue
		}
		for _, file := range p.Syntax {
			for _, decl := range file.Decls {
				out = append(out, declStructs(p, decl)...)
			}
		}
	}
	return out
}

func declStructs(p *packages.Package, decl ast.Decl) []structDecl {
	gd, ok := decl.(*ast.GenDecl)
	if !ok || gd.Tok != token.TYPE {
		return nil
	}
	var out []structDecl
	for _, spec := range gd.Specs {
		ts, _ := spec.(*ast.TypeSpec)
		obj, _ := p.TypesInfo.Defs[ts.Name].(*types.TypeName)
		if obj == nil {
			continue
		}
		st, ok := obj.Type().Underlying().(*types.Struct)
		if !ok {
			continue
		}
		doc := ts.Doc
		if doc == nil && len(gd.Specs) == 1 {
			doc = gd.Doc
		}
		out = append(out, structDecl{obj: obj, st: st, doc: doc, pkg: p})
	}
	return out
}

// embeddedTypes returns every named type embedded in a declared struct.
func embeddedTypes(decls []structDecl) map[*types.TypeName]bool {
	out := map[*types.TypeName]bool{}
	for i := range decls {
		st := decls[i].st
		for j := range st.NumFields() {
			if f := st.Field(j); f.Embedded() {
				if n := namedOf(f.Type()); n != nil {
					out[n.Obj()] = true
				}
			}
		}
	}
	return out
}

func toEntity(
	d *structDecl, anns mml.Annotations, byObj map[*types.TypeName]*structDecl,
	rel func(token.Position) mml.Position,
) (mml.Entity, bool) {
	fields := flatten(d.st, d.pkg.Fset, rel, map[*types.Struct]bool{})
	if len(anns) == 0 && !slices.ContainsFunc(fields, func(f goField) bool { return f.stored }) {
		return mml.Entity{}, false
	}
	e := mml.Entity{
		ID:   "go:" + d.obj.Pkg().Path() + "." + d.obj.Name(),
		Kind: mml.KindStruct, Source: mml.SourceGo, Name: d.obj.Name(),
		Doc: docText(d.doc), Annotations: anns, Pos: rel(d.pkg.Fset.Position(d.obj.Pos())),
	}
	for i := range fields {
		gf := &fields[i]
		e.Fields = append(e.Fields, gf.field)
		if gf.pk {
			e.PrimaryKey = append(e.PrimaryKey, gf.field.Name)
		}
		if gf.refTarget != nil && gf.refTarget != d.obj && byObj[gf.refTarget] != nil {
			e.Refs = append(e.Refs, mml.Ref{
				Fields: []string{gf.field.Name}, Target: gf.refTarget.Name(), Pos: gf.field.Pos,
			})
		}
	}
	if len(e.PrimaryKey) == 0 {
		if f, ok := e.Field("id"); ok {
			e.PrimaryKey = []string{f.Name}
		}
	}
	e.Refs = append(e.Refs, inferredRefs(&e, d, byObj)...)
	return e, true
}

// flatten returns st's exported, persisted fields in declaration order,
// expanding embedded structs wherever they are declared. seen stops a cycle.
func flatten(
	st *types.Struct, fset *token.FileSet, rel func(token.Position) mml.Position,
	seen map[*types.Struct]bool,
) []goField {
	seen[st] = true
	var out []goField
	for i := range st.NumFields() {
		v, tag := st.Field(i), reflect.StructTag(st.Tag(i))
		if v.Embedded() {
			if inner, ok := deref(v.Type()).Underlying().(*types.Struct); ok && !seen[inner] {
				out = append(out, flatten(inner, fset, rel, seen)...)
			}
			continue
		}
		if !v.Exported() {
			continue
		}
		if gf, ok := makeField(v, tag, rel(fset.Position(v.Pos()))); ok {
			out = append(out, gf)
		}
	}
	return out
}

func makeField(v *types.Var, tag reflect.StructTag, pos mml.Position) (goField, bool) {
	name, stored, omitempty := tagName(tag)
	if name == "-" {
		return goField{}, false
	}
	if name == "" {
		name = snake(v.Name())
	}
	raw := types.TypeString(v.Type(), func(p *types.Package) string { return p.Name() })
	lt, nullable := fieldType(v.Type(), raw)
	gf := goField{
		field: mml.Field{
			Name: name, Type: lt, RawType: raw, Nullable: nullable || omitempty, Pos: pos,
			Annotations: mml.ParseTag(tag.Get("mml"), pos),
		},
		stored:    stored || tag.Get("mml") != "",
		refTarget: structRef(v.Type(), lt),
	}
	if gf.refTarget != nil && lt == mml.TypeUnknown {
		gf.field.Type = mml.TypeObject
	}
	_, gf.pk = gf.field.Annotations.Get("pk")
	gf.pk = gf.pk || strings.Contains(tag.Get("gorm"), "primaryKey")
	return gf, true
}

// fieldType maps a field's type to a logical type: by its printed name for
// well-known types (time.Time, sql.NullString), else by its underlying basic
// type (type Status string).
func fieldType(t types.Type, raw string) (mml.LogicalType, bool) {
	lt, nullable := logicalType(raw)
	if lt == mml.TypeUnknown {
		lt = basicType(t)
	}
	return lt, nullable
}

// structRef returns the struct type a field of type t references: t itself,
// or the element of a slice of it. lt is the field's logical type, which is
// unknown for a struct value and array for a slice.
func structRef(t types.Type, lt mml.LogicalType) *types.TypeName {
	if lt != mml.TypeUnknown && lt != mml.TypeArray {
		return nil
	}
	n := namedOf(elem(t))
	if n == nil {
		return nil
	}
	if _, isStruct := n.Underlying().(*types.Struct); !isStruct {
		return nil
	}
	return n.Obj()
}

// inferredRefs adds a reference for each XxxID field when a struct Xxx is
// declared in the same package, marked Inferred because only the name links
// them.
func inferredRefs(e *mml.Entity, d *structDecl, byObj map[*types.TypeName]*structDecl) []mml.Ref {
	var out []mml.Ref
	for i := range e.Fields {
		f := &e.Fields[i]
		stem, ok := strings.CutSuffix(mml.Canonical(f.Name), "_id")
		if !ok ||
			slices.ContainsFunc(
				e.Refs,
				func(r mml.Ref) bool { return slices.Contains(r.Fields, f.Name) },
			) {
			continue
		}
		for obj := range byObj {
			if obj.Pkg() == d.obj.Pkg() && obj != d.obj && mml.Canonical(obj.Name()) == stem {
				out = append(
					out,
					mml.Ref{
						Fields:   []string{f.Name},
						Target:   obj.Name(),
						Inferred: true,
						Pos:      f.Pos,
					},
				)
				break
			}
		}
	}
	return out
}

// directives reads //mml:key value lines from a doc comment. They are read
// from the raw comment list, because CommentGroup.Text drops directive-style
// lines.
func directives(
	doc *ast.CommentGroup,
	fset *token.FileSet,
	rel func(token.Position) mml.Position,
) mml.Annotations {
	out := mml.Annotations{}
	if doc == nil {
		return out
	}
	for _, c := range doc.List {
		text, ok := strings.CutPrefix(c.Text, "//")
		if !ok || !strings.HasPrefix(text, "mml:") {
			continue
		}
		for k, v := range mml.ParseLines(text, rel(fset.Position(c.Pos()))) {
			out[k] = v
		}
	}
	return out
}

func docText(doc *ast.CommentGroup) string {
	if doc == nil {
		return ""
	}
	return strings.TrimSpace(doc.Text())
}

// basicType maps a named type whose underlying type is basic (type Status
// string) to that basic type's logical type.
func basicType(t types.Type) mml.LogicalType {
	b, ok := deref(t).Underlying().(*types.Basic)
	if !ok {
		return mml.TypeUnknown
	}
	switch {
	case b.Info()&types.IsString != 0:
		return mml.TypeString
	case b.Info()&types.IsInteger != 0:
		return mml.TypeInt
	case b.Info()&types.IsFloat != 0:
		return mml.TypeFloat
	case b.Info()&types.IsBoolean != 0:
		return mml.TypeBool
	default:
		return mml.TypeUnknown
	}
}

// deref returns the type a pointer points to, or t.
func deref(t types.Type) types.Type {
	if p, ok := types.Unalias(t).(*types.Pointer); ok {
		return p.Elem()
	}
	return t
}

// elem returns the element type of a pointer, slice or array, or t.
func elem(t types.Type) types.Type {
	t = deref(t)
	switch s := types.Unalias(t).(type) {
	case *types.Slice:
		return deref(s.Elem())
	case *types.Array:
		return deref(s.Elem())
	default:
		return t
	}
}

func namedOf(t types.Type) *types.Named {
	n, _ := types.Unalias(deref(t)).(*types.Named)
	return n
}
