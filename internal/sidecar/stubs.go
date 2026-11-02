package sidecar

import (
	"fmt"

	"github.com/goccy/go-yaml"

	mml "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// RenderStubs writes stubs as an mml.yaml fragment, in the order given:
//
//	entities:
//	  order:
//	    grain: one row per order fp=1a2b3c4d
//	    fields:
//	      total:
//	        measure: additive
//
// The fragment is for a person to review and merge; it never overwrites an
// existing mml.yaml.
func RenderStubs(stubs []mml.Stub) ([]byte, error) {
	var entities yaml.MapSlice
	index := map[string]int{}
	for _, s := range stubs {
		i, ok := index[s.Entity]
		if !ok {
			i = len(entities)
			index[s.Entity] = i
			entities = append(entities, yaml.MapItem{Key: s.Entity, Value: yaml.MapSlice{}})
		}
		entry := entities[i].Value.(yaml.MapSlice)
		if s.Field == "" {
			entry = append(entry, yaml.MapItem{Key: s.Key, Value: s.Value})
		} else {
			entry = addFieldStub(entry, s)
		}
		entities[i].Value = entry
	}
	out, err := yaml.Marshal(yaml.MapSlice{{Key: "entities", Value: entities}})
	if err != nil {
		return nil, fmt.Errorf("render stubs: %w", err)
	}
	return out, nil
}

// addFieldStub adds a field's stub under the entity's fields key.
func addFieldStub(entry yaml.MapSlice, s mml.Stub) yaml.MapSlice {
	fi := -1
	for i, item := range entry {
		if item.Key == "fields" {
			fi = i
		}
	}
	if fi < 0 {
		fi = len(entry)
		entry = append(entry, yaml.MapItem{Key: "fields", Value: yaml.MapSlice{}})
	}
	fields := entry[fi].Value.(yaml.MapSlice)
	found := -1
	for i, item := range fields {
		if item.Key == s.Field {
			found = i
		}
	}
	if found < 0 {
		found = len(fields)
		fields = append(fields, yaml.MapItem{Key: s.Field, Value: yaml.MapSlice{}})
	}
	fields[found].Value = append(
		fields[found].Value.(yaml.MapSlice),
		yaml.MapItem{Key: s.Key, Value: s.Value},
	)
	entry[fi].Value = fields
	return entry
}
