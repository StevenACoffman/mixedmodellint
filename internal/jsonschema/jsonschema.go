// Package jsonschema loads JSON Schema documents into the mixedmodellint IR,
// using only encoding/json. The root schema and each $defs or definitions
// entry that describes an object become entities; x-mml-* keywords become
// annotations. $ref is resolved by name only: "#/$defs/Line" and
// "line.schema.json" both refer to the entity named Line.
package jsonschema

import (
	"path"
	"slices"
	"strconv"
	"strings"

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
		if err != nil {
			out.Problems = append(out.Problems, mml.Problem{
				Message: "JSON does not parse: " + err.Error(),
				Pos:     mml.Position{File: f.Path, Line: 1},
			})
			continue
		}
		d := &doc{path: f.Path, lines: lineStarts(f.Src)}
		out.Entities = append(out.Entities, entities(d, root)...)
	}
	return out
}

func entities(d *doc, root *node) []mml.Entity {
	var out []mml.Entity
	if isObjectSchema(root) {
		name := root.get("title").str()
		if name == "" {
			name = baseName(d.path)
		}
		out = append(out, entity(d, "json:"+d.path+"#", name, root, root.off))
	}
	for _, section := range []string{"$defs", "definitions"} {
		defs := root.get(section)
		if defs == nil {
			continue
		}
		for _, m := range defs.members {
			if isObjectSchema(m.val) {
				id := "json:" + d.path + "#/" + section + "/" + m.key
				out = append(out, entity(d, id, m.key, m.val, m.keyOff))
			}
		}
	}
	return out
}

func isObjectSchema(n *node) bool {
	if n == nil || !n.isObj {
		return false
	}
	return n.get("type").str() == "object" || n.get("properties") != nil
}

func entity(d *doc, id, name string, n *node, off int) mml.Entity {
	e := mml.Entity{
		ID: id, Kind: mml.KindSchema, Source: mml.SourceJSONSchema, Name: name,
		Doc: n.get("description").str(), Pos: d.pos(off), Annotations: annotations(d, n),
	}
	required := stringValues(n.get("required"))
	for _, m := range n.get("properties").membersOrNil() {
		f, ref := field(d, m, slices.Contains(required, m.key))
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

func field(d *doc, m member, required bool) (mml.Field, string) {
	p := m.val
	types := typeList(p.get("type"))
	f := mml.Field{
		Name:        m.key,
		Doc:         p.get("description").str(),
		Nullable:    !required || slices.Contains(types, "null"),
		Enum:        stringValues(p.get("enum")),
		Pos:         d.pos(m.keyOff),
		Annotations: annotations(d, p),
	}
	ref := refName(p.get("$ref").str())
	nonNull := slices.DeleteFunc(slices.Clone(types), func(t string) bool { return t == "null" })
	switch {
	case ref != "":
		f.Type, f.RawType = mml.TypeObject, "$ref:"+ref
	case len(nonNull) == 1 && nonNull[0] == "array":
		f.Type, f.RawType = mml.TypeArray, "array"
		ref = refName(p.get("items").get("$ref").str())
	case len(nonNull) == 1:
		f.Type = scalarType(nonNull[0], p.get("format").str())
		f.RawType = nonNull[0]
		if format := p.get("format").str(); format != "" {
			f.RawType += " (format " + format + ")"
		}
	}
	return f, ref
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

func typeList(n *node) []string {
	if n == nil {
		return nil
	}
	if n.isArr {
		return stringValues(n)
	}
	return []string{n.str()}
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
