// Package goast loads persisted Go structs into the mixedmodellint IR, using
// the standard library's go/parser and go/ast. It reads syntax only, not
// types, so it needs no build of the code under analysis: a struct is an
// entity when one of its fields has a storage tag (db, gorm, bun, sql, bson,
// parquet, avro) or an mml tag, or when its doc comment carries an //mml:
// directive. A struct with only json tags is a payload, not stored data.
package goast

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"reflect"
	"slices"
	"strings"

	mml "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// File is one Go source file's path and contents.
type File struct {
	Path string
	Src  []byte
}

// structDecl is one struct type found while scanning, before embedded structs
// are expanded.
type structDecl struct {
	pkgDir string
	pkg    string
	name   string
	doc    *ast.CommentGroup
	typ    *ast.StructType
	pos    mml.Position
	fset   *token.FileSet
	file   string
}

// persistenceTags are the struct tag keys that mark a field as stored or
// serialized, in the order their names win: a db tag names the column even
// when a json tag also exists.
func persistenceTags() []string {
	return []string{"db", "gorm", "bun", "sql", "bson", "parquet", "avro", "json"}
}

// Load parses files and returns one entity per persisted struct. Embedded
// structs are expanded when they are declared in the same directory.
//
// Requires: files are Go sources; _test.go files should be left out.
// Ensures:  a file that fails to parse contributes a Problem and nothing else.
func Load(files []File) mml.Source {
	var out mml.Source
	var decls []structDecl
	for i := range files {
		ds, problem := scan(&files[i])
		if problem != nil {
			out.Problems = append(out.Problems, *problem)
			continue
		}
		decls = append(decls, ds...)
	}
	known := map[string]*structDecl{}
	for i := range decls {
		known[decls[i].pkgDir+"."+decls[i].name] = &decls[i]
	}
	mixins := embeddedNames(decls)
	for i := range decls {
		d := &decls[i]
		// A struct that is only embedded (an Audit mixin) is part of the
		// structs that embed it, not an entity of its own, unless it says so.
		if mixins[d.pkgDir+"."+d.name] && len(directives(d.doc, d.fset)) == 0 {
			continue
		}
		if e, ok := toEntity(d, known); ok {
			out.Entities = append(out.Entities, e)
		}
	}
	return out
}

// embeddedNames returns the "dir.Name" of every struct embedded in another.
func embeddedNames(decls []structDecl) map[string]bool {
	out := map[string]bool{}
	for i := range decls {
		for _, f := range decls[i].typ.Fields.List {
			if len(f.Names) == 0 {
				out[decls[i].pkgDir+"."+strings.TrimPrefix(typeString(f.Type), "*")] = true
			}
		}
	}
	return out
}

func scan(f *File) ([]structDecl, *mml.Problem) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(
		fset,
		f.Path,
		f.Src,
		parser.ParseComments|parser.SkipObjectResolution,
	)
	if err != nil {
		return nil, &mml.Problem{
			Message: "Go source does not parse: " + err.Error(),
			Pos:     mml.Position{File: f.Path, Line: 1},
		}
	}
	var out []structDecl
	for _, d := range file.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, _ := spec.(*ast.TypeSpec)
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				continue
			}
			doc := ts.Doc
			if doc == nil && len(gd.Specs) == 1 {
				doc = gd.Doc
			}
			out = append(out, structDecl{
				pkgDir: path.Dir(f.Path), pkg: file.Name.Name, name: ts.Name.Name,
				doc: doc, typ: st, pos: position(fset, ts.Name.Pos()), fset: fset, file: f.Path,
			})
		}
	}
	return out, nil
}

func toEntity(d *structDecl, known map[string]*structDecl) (mml.Entity, bool) {
	anns := directives(d.doc, d.fset)
	fields := collectFields(d, known, map[string]bool{})
	if len(anns) == 0 && !slices.ContainsFunc(fields, func(f goField) bool { return f.tagged }) {
		return mml.Entity{}, false
	}
	e := mml.Entity{
		ID: "go:" + d.pkgDir + "." + d.name, Kind: mml.KindStruct, Source: mml.SourceGo,
		Name: d.name, Doc: docText(d.doc), Annotations: anns, Pos: d.pos,
	}
	for i := range fields {
		gf := &fields[i]
		e.Fields = append(e.Fields, gf.field)
		if gf.pk {
			e.PrimaryKey = append(e.PrimaryKey, gf.field.Name)
		}
		if gf.refTarget != "" {
			e.Refs = append(e.Refs, mml.Ref{
				Fields: []string{gf.field.Name}, Target: gf.refTarget, Pos: gf.field.Pos,
			})
		}
	}
	if len(e.PrimaryKey) == 0 {
		if f, ok := e.Field("id"); ok {
			e.PrimaryKey = []string{f.Name}
		}
	}
	e.Refs = append(e.Refs, inferredRefs(&e, d, known)...)
	return e, true
}

// inferredRefs adds a reference for each XxxID field when a struct Xxx is
// declared in the same directory, marked Inferred because only the name links
// them.
func inferredRefs(e *mml.Entity, d *structDecl, known map[string]*structDecl) []mml.Ref {
	var out []mml.Ref
	for i := range e.Fields {
		f := &e.Fields[i]
		c := mml.Canonical(f.Name)
		stem, ok := strings.CutSuffix(c, "_id")
		if !ok ||
			slices.ContainsFunc(
				e.Refs,
				func(r mml.Ref) bool { return slices.Contains(r.Fields, f.Name) },
			) {
			continue
		}
		for _, k := range known {
			if k.pkgDir == d.pkgDir && mml.Canonical(k.name) == stem && k.name != d.name {
				out = append(
					out,
					mml.Ref{Fields: []string{f.Name}, Target: k.name, Inferred: true, Pos: f.Pos},
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
func directives(doc *ast.CommentGroup, fset *token.FileSet) mml.Annotations {
	out := mml.Annotations{}
	if doc == nil {
		return out
	}
	for _, c := range doc.List {
		text, ok := strings.CutPrefix(c.Text, "//")
		if !ok || !strings.HasPrefix(text, "mml:") {
			continue
		}
		for k, v := range mml.ParseLines(text, position(fset, c.Pos())) {
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

func position(fset *token.FileSet, p token.Pos) mml.Position {
	pp := fset.Position(p)
	return mml.Position{File: pp.Filename, Line: pp.Line, Col: pp.Column}
}

// tagName returns the persisted name a field's tags give it, whether a
// storage tag (anything but json) is present, and whether a tag says the
// value may be omitted. A "-" name means the field is not persisted.
//
// A json tag names the field but does not make the struct an entity: a
// struct that is only serialized is an API payload or a report, not stored
// data, and asking it for a grain would be noise.
func tagName(tag reflect.StructTag) (name string, stored, omitempty bool) {
	for _, key := range persistenceTags() {
		v, ok := tag.Lookup(key)
		if !ok {
			continue
		}
		stored = stored || key != "json"
		n, opts := tagValue(key, v)
		omitempty = omitempty || strings.Contains(opts, "omitempty")
		if name == "" {
			name = n
		}
	}
	return name, stored, omitempty
}

// tagValue splits one tag's value into the column name and its options. gorm
// writes `gorm:"column:x;primaryKey"`; the others write `json:"x,omitempty"`.
func tagValue(key, v string) (name, opts string) {
	if key == "gorm" {
		for part := range strings.SplitSeq(v, ";") {
			if col, ok := strings.CutPrefix(part, "column:"); ok {
				name = col
			}
		}
		return name, v
	}
	name, opts, _ = strings.Cut(v, ",")
	return name, opts
}
