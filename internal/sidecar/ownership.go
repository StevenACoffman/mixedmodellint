package sidecar

import (
	"strings"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"

	mml "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// ParseOwnership reads ownership records (rule_plan.md §1.13). With owner
// empty, src is mml.yaml and the records are its owners section, keyed by
// owner; otherwise src is model/ownership/<owner>.yaml and holds one record:
//
//	model: [orders, order_*]
//	responsibilities: [definitions, data_quality]
//	decisions:
//	  - {name: approve_breaking_change, without_signoff: true}
//	granted_by: role:vp-data
//
// "ownership: liability" in a record marks an owner named without authority.
func ParseOwnership(path string, src []byte, owner string) ([]mml.OwnershipRecord, []mml.Problem) {
	file, err := parser.ParseBytes(src, 0)
	if err != nil {
		return nil, []mml.Problem{{
			Message: "ownership record does not parse: " + err.Error(),
			Pos:     mml.Position{File: path, Line: 1},
		}}
	}
	if len(file.Docs) == 0 || file.Docs[0].Body == nil {
		return nil, nil
	}
	body := file.Docs[0].Body
	if owner != "" {
		return []mml.OwnershipRecord{
			ownership(path, owner, body, mml.Position{File: path, Line: 1}),
		}, nil
	}
	var out []mml.OwnershipRecord
	for _, top := range pairs(body) {
		if keyOf(top) != "owners" {
			continue
		}
		for _, o := range pairs(top.Value) {
			out = append(out, ownership(path, keyOf(o), o.Value, pos(path, o)))
		}
	}
	return out, nil
}

func ownership(path, owner string, n ast.Node, at mml.Position) mml.OwnershipRecord {
	rec := mml.OwnershipRecord{Owner: owner, Pos: at}
	for _, kv := range pairs(n) {
		switch keyOf(kv) {
		case "model":
			rec.Model = items(kv.Value)
		case "responsibilities":
			rec.Responsibilities = items(kv.Value)
		case "decisions":
			rec.Decisions = decisions(kv.Value)
		case "granted_by":
			rec.GrantedBy = text(kv.Value)
		case "ownership":
			rec.Liability = strings.EqualFold(text(kv.Value), "liability")
		}
	}
	if rec.Pos.File == "" {
		rec.Pos = mml.Position{File: path}
	}
	return rec
}

// decisions reads a decisions list whose items are names or
// {name, without_signoff} mappings.
func decisions(n ast.Node) []mml.Decision {
	seq, ok := n.(*ast.SequenceNode)
	if !ok {
		return nil
	}
	out := make([]mml.Decision, 0, len(seq.Values))
	for _, v := range seq.Values {
		kvs := pairs(v)
		if len(kvs) == 0 {
			out = append(out, mml.Decision{Name: text(v)})
			continue
		}
		var d mml.Decision
		for _, kv := range kvs {
			switch keyOf(kv) {
			case "name":
				d.Name = text(kv.Value)
			case "without_signoff":
				d.WithoutSignoff = strings.EqualFold(text(kv.Value), "true")
			}
		}
		out = append(out, d)
	}
	return out
}
