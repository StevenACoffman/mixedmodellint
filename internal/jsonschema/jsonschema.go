// Package jsonschema loads JSON Schema documents into the mixedmodellint IR.
// It wraps github.com/atombender/go-jsonschema/pkg/schemas, which interprets
// the schema: types, required, $ref, items, formats, enums, $defs and the
// legacy definitions keyword, boolean schemas. That library keeps neither
// source positions nor x-* extension keywords, so a small offset-tracking
// reader (tree.go) supplies the two things it drops: where each property is,
// and the x-mml-* annotations.
//
// The root schema and each $defs or definitions entry that describes an
// object become entities. $ref is resolved by name only: "#/$defs/Line" and
// "line.schema.json" both refer to the entity named Line.
package jsonschema

import (
	"bytes"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/atombender/go-jsonschema/pkg/schemas"

	mml "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// annotationPrefix marks a keyword as an mml annotation.
const annotationPrefix = "x-mml-"

// File is one JSON file's path and contents.
type File struct {
	Path string
	Src  []byte
}

// doc is one file being loaded, for positions.
type doc struct {
	path  string
	lines []int
}

// IsSchema reports whether a JSON file is a JSON Schema the linter should
// read: its name ends in .schema.json, or its top-level object declares
// "$schema". Other JSON (package.json, fixtures) is not an entity source.
func IsSchema(p string, src []byte) bool {
	if strings.HasSuffix(p, ".schema.json") {
		return true
	}
	head := src[:min(len(src), 512)]
	return strings.Contains(string(head), `"$schema"`)
}

// Load reads every file and returns its entities. A file that is not valid
// JSON contributes a Problem.
func Load(files []File) mml.Source {
	var out mml.Source
	for i := range files {
		f := &files[i]
		root, err := parseTree(f.Src)
		if err == nil {
			var sch *schemas.Schema
			if sch, err = schemas.FromJSONReader(bytes.NewReader(f.Src)); err == nil {
				d := &doc{path: f.Path, lines: lineStarts(f.Src)}
				out.Entities = append(out.Entities, entities(d, root, sch)...)
				continue
			}
		}
		out.Problems = append(out.Problems, mml.Problem{
			Message: "JSON Schema does not parse: " + err.Error(),
			Pos:     mml.Position{File: f.Path, Line: 1},
		})
	}
	return out
}

// entities returns the root schema and each object definition as entities.
// The schema gives meaning; the tree gives source order and positions.
func entities(d *doc, root *node, sch *schemas.Schema) []mml.Entity {
	var out []mml.Entity
	if rootType := (*schemas.Type)(sch.ObjectAsType); isObject(rootType) {
		name := rootType.Title
		if name == "" {
			name = baseName(d.path)
		}
		out = append(out, entity(d, "json:"+d.path+"#", name, rootType, root, root.off))
	}
	for _, section := range []string{"$defs", "definitions"} {
		for _, m := range root.get(section).membersOrNil() {
			if t := sch.Definitions[m.key]; isObject(t) {
				id := "json:" + d.path + "#/" + section + "/" + m.key
				out = append(out, entity(d, id, m.key, t, m.val, m.keyOff))
			}
		}
	}
	return out
}

// isObject reports whether t describes an object. go-jsonschema infers
// "object" from properties for nested schemas but not for the root, so a
// schema with properties counts either way.
func isObject(t *schemas.Type) bool {
	return t != nil && (slices.Contains(t.Type, "object") || len(t.Properties) > 0)
}

// entity builds one entity from schema t, whose source is node at off.
// Properties are taken in source order from the tree, since the schema holds
// them in a map.
func entity(d *doc, id, name string, t *schemas.Type, n *node, off int) mml.Entity {
	e := mml.Entity{
		ID: id, Kind: mml.KindSchema, Source: mml.SourceJSONSchema, Name: name,
		Doc: t.Description, Pos: d.pos(off), Annotations: annotations(d, n),
	}
	for _, m := range n.get("properties").membersOrNil() {
		p := t.Properties[m.key]
		if p == nil {
			continue
		}
		f, ref := field(d, m, p, slices.Contains(t.Required, m.key))
		e.Fields = append(e.Fields, f)
		if ref != "" {
			e.Refs = append(e.Refs, mml.Ref{Fields: []string{f.Name}, Target: ref, Pos: f.Pos})
		}
	}
	if key, ok := e.Annotations["key"]; ok {
		e.PrimaryKey = splitList(key.Value)
	}
	return e
}

// field converts property p, declared at member m, and returns the entity
// its $ref (or its items' $ref) names, if any.
func field(d *doc, m member, p *schemas.Type, required bool) (mml.Field, string) {
	f := mml.Field{
		Name:        m.key,
		Doc:         p.Description,
		Nullable:    !required || slices.Contains(p.Type, "null"),
		Enum:        anyStrings(p.Enum),
		Pos:         d.pos(m.keyOff),
		Annotations: annotations(d, m.val),
	}
	ref := refName(p.Ref)
	nonNull := slices.DeleteFunc(
		slices.Clone([]string(p.Type)),
		func(t string) bool { return t == "null" },
	)
	switch {
	case ref != "":
		f.Type, f.RawType = mml.TypeObject, "$ref:"+ref
	case len(nonNull) == 1 && nonNull[0] == "array":
		f.Type, f.RawType = mml.TypeArray, "array"
		if p.Items != nil {
			ref = refName(p.Items.Ref)
		}
	case len(nonNull) == 1:
		f.Type, f.RawType = scalarType(nonNull[0], p.Format), nonNull[0]
		if p.Format != "" {
			f.RawType += " (format " + p.Format + ")"
		}
	}
	return f, ref
}

// anyStrings returns the string members of an enum.
func anyStrings(values []any) []string {
	var out []string
	for _, v := range values {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func scalarType(t, format string) mml.LogicalType {
	switch t {
	case "string":
		formats := map[string]mml.LogicalType{
			"date": mml.TypeDate, "date-time": mml.TypeTimestamptz, "time": mml.TypeTime,
			"duration": mml.TypeInterval, "uuid": mml.TypeUUID, "byte": mml.TypeBytes,
		}
		if lt, ok := formats[format]; ok {
			return lt
		}
		return mml.TypeString
	case "integer":
		return mml.TypeInt
	case "number":
		return mml.TypeFloat
	case "boolean":
		return mml.TypeBool
	case "object":
		return mml.TypeObject
	default:
		return mml.TypeUnknown
	}
}

// annotations collects x-mml-* keywords. A list value is joined with ", " and
// an object or boolean value is kept as its JSON-ish text, since annotation
// values are plain strings (rule_plan.md §2).
func annotations(d *doc, n *node) mml.Annotations {
	out := mml.Annotations{}
	for _, m := range n.membersOrNil() {
		key, ok := strings.CutPrefix(m.key, annotationPrefix)
		if !ok {
			continue
		}
		out[key] = mml.Annotation{Value: scalarText(m.val), Pos: d.pos(m.keyOff)}
	}
	return out
}

func scalarText(n *node) string {
	switch {
	case n.isArr:
		return strings.Join(stringValues(n), ", ")
	case n.isObj:
		var parts []string
		for _, m := range n.members {
			parts = append(parts, m.key+"="+scalarText(m.val))
		}
		return strings.Join(parts, " ")
	}
	switch v := n.value.(type) {
	case string:
		return v
	case bool:
		if v {
			return "true"
		}
		return "false"
	case nil:
		return ""
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return ""
	}
}

func (n *node) membersOrNil() []member {
	if n == nil {
		return nil
	}
	return n.members
}

func stringValues(n *node) []string {
	if n == nil || !n.isArr {
		return nil
	}
	var out []string
	for _, it := range n.items {
		if s, ok := it.value.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func splitList(s string) []string {
	var out []string
	for part := range strings.SplitSeq(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// refName returns the entity a $ref names: the last path segment of the
// fragment, or the file name without its extensions.
func refName(ref string) string {
	if ref == "" {
		return ""
	}
	if _, frag, ok := strings.Cut(ref, "#"); ok && frag != "" {
		return path.Base(frag)
	}
	return baseName(strings.TrimSuffix(ref, "#"))
}

func baseName(p string) string {
	b := path.Base(p)
	b = strings.TrimSuffix(b, ".json")
	return strings.TrimSuffix(b, ".schema")
}

func lineStarts(src []byte) []int {
	starts := []int{0}
	for i, b := range src {
		if b == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

func (d *doc) pos(off int) mml.Position {
	i, found := slices.BinarySearch(d.lines, off)
	if !found {
		i--
	}
	return mml.Position{File: d.path, Line: i + 1, Col: off - d.lines[i] + 1}
}
