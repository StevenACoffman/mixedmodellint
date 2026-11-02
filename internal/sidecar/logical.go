package sidecar

import (
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"

	mml "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// ParseLogical reads a logical-model record, model/logical/<entity>.yaml
// (rule_plan.md §1.1):
//
//	entity: order_line
//	attributes: [order_id, line_no, amount]
//	keys: [order_id, line_no]
//
// A record that does not parse, or names no entity, yields a Problem and no
// record, so the rest of the run still happens.
func ParseLogical(path string, src []byte) ([]mml.LogicalRecord, []mml.Problem) {
	file, err := parser.ParseBytes(src, 0)
	if err != nil {
		return nil, []mml.Problem{{
			Message: "logical-model record does not parse: " + err.Error(),
			Pos:     mml.Position{File: path, Line: 1},
		}}
	}
	if len(file.Docs) == 0 || file.Docs[0].Body == nil {
		return nil, nil
	}
	rec := mml.LogicalRecord{Pos: mml.Position{File: path, Line: 1}}
	for _, kv := range pairs(file.Docs[0].Body) {
		switch keyOf(kv) {
		case "entity":
			rec.Entity, rec.Pos = text(kv.Value), pos(path, kv)
		case "attributes":
			rec.Attributes = items(kv.Value)
		case "keys":
			rec.Keys = items(kv.Value)
		}
	}
	if rec.Entity == "" {
		return nil, []mml.Problem{{
			Message: "logical-model record names no entity (add entity: <name>)",
			Pos:     rec.Pos,
		}}
	}
	return []mml.LogicalRecord{rec}, nil
}

// items returns a sequence's scalar values, or a lone scalar as one item.
func items(n ast.Node) []string {
	seq, ok := n.(*ast.SequenceNode)
	if !ok {
		if s := text(n); s != "" {
			return []string{s}
		}
		return nil
	}
	out := make([]string, 0, len(seq.Values))
	for _, v := range seq.Values {
		out = append(out, text(v))
	}
	return out
}
