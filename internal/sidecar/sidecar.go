// Package sidecar reads mml.yaml, the catalog of annotations for entities
// whose source cannot carry them inline (rule_plan.md §2). It wraps
// github.com/goccy/go-yaml, whose AST keeps line numbers, so an annotation
// conflict can point at the sidecar line.
//
// The file has the shape
//
//	entities:
//	  orders:                    # entity ID, name or canonical name
//	    grain: one row per order
//	    synonyms: [purchase, sale]
//	    fields:
//	      amount:
//	        unit: USD
package sidecar

import (
	"strings"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"

	mml "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// Parse reads an mml.yaml document. A document that is not valid YAML, or
// whose entities key is not a mapping, yields Problems and an empty Sidecar,
// so the rest of the run still happens.
func Parse(path string, src []byte) (mml.Sidecar, []mml.Problem) {
	sc := mml.Sidecar{File: path, Entities: map[string]mml.SidecarEntity{}}
	file, err := parser.ParseBytes(src, 0)
	if err != nil {
		return sc, []mml.Problem{{
			Message: "mml.yaml does not parse: " + err.Error(),
			Pos:     mml.Position{File: path, Line: 1},
		}}
	}
	if len(file.Docs) == 0 || file.Docs[0].Body == nil {
		return sc, nil
	}
	for _, top := range pairs(file.Docs[0].Body) {
		if keyOf(top) != "entities" {
			continue
		}
		for _, ent := range pairs(top.Value) {
			sc.Entities[keyOf(ent)] = entity(path, ent.Value)
		}
	}
	return sc, nil
}

func entity(path string, n ast.Node) mml.SidecarEntity {
	se := mml.SidecarEntity{Annotations: mml.Annotations{}, Fields: map[string]mml.Annotations{}}
	for _, kv := range pairs(n) {
		key := keyOf(kv)
		if key == "fields" {
			for _, f := range pairs(kv.Value) {
				se.Fields[keyOf(f)] = annotations(path, f.Value)
			}
			continue
		}
		se.Annotations[key] = mml.Annotation{Value: text(kv.Value), Pos: pos(path, kv)}
	}
	return se
}

func annotations(path string, n ast.Node) mml.Annotations {
	out := mml.Annotations{}
	for _, kv := range pairs(n) {
		out[keyOf(kv)] = mml.Annotation{Value: text(kv.Value), Pos: pos(path, kv)}
	}
	return out
}

// pairs returns a mapping's key/value pairs. A one-key mapping can parse as a
// bare MappingValueNode, so both shapes are accepted; anything else has none.
func pairs(n ast.Node) []*ast.MappingValueNode {
	switch m := n.(type) {
	case *ast.MappingNode:
		return m.Values
	case *ast.MappingValueNode:
		return []*ast.MappingValueNode{m}
	default:
		return nil
	}
}

func keyOf(kv *ast.MappingValueNode) string {
	return strings.TrimSpace(kv.Key.String())
}

// text renders a value as an annotation string: a scalar as written, a
// sequence joined with ", ".
func text(n ast.Node) string {
	switch v := n.(type) {
	case *ast.StringNode:
		return v.Value
	case *ast.SequenceNode:
		parts := make([]string, 0, len(v.Values))
		for _, item := range v.Values {
			parts = append(parts, text(item))
		}
		return strings.Join(parts, ", ")
	case nil:
		return ""
	default:
		return strings.TrimSpace(n.String())
	}
}

func pos(path string, kv *ast.MappingValueNode) mml.Position {
	tk := kv.Key.GetToken()
	if tk == nil || tk.Position == nil {
		return mml.Position{File: path}
	}
	return mml.Position{File: path, Line: tk.Position.Line, Col: tk.Position.Column}
}
