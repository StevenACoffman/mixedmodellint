package mixedmodellint

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
)

// lockVersion is the format version written to and required of a lock file.
const lockVersion = 1

// Lock is the committed schema lock, mml.lock.json (rule_plan.md §6, M3):
// the structure of every entity, sorted so it diffs well in review. Comparing
// the current IR with it shows what a change adds, removes and reshapes
// without git history, so the designs that need history run in pre-commit
// hooks and shallow clones. The zero Lock holds no entities.
type Lock struct {
	entities []lockedEntity
}

type lockFile struct {
	Version  int            `json:"version"`
	Entities []lockedEntity `json:"entities"`
}

type lockedEntity struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Kind        string        `json:"kind"`
	Source      string        `json:"source"`
	Fingerprint string        `json:"fingerprint"`
	Fields      []lockedField `json:"fields"`
	PrimaryKey  []string      `json:"primary_key,omitempty"`
	Uniques     [][]string    `json:"uniques,omitempty"`
	Refs        []lockedRef   `json:"refs,omitempty"`
}

type lockedField struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable,omitempty"`
}

type lockedRef struct {
	Fields []string `json:"fields"`
	Target string   `json:"target"`
}

// lockChange is how one entity differs from its locked shape. current is nil
// when the entity was removed, locked is nil when it was added.
type lockChange struct {
	locked, current *Entity
	added, removed  []string // field names
	retyped         []string // field names whose type or nullability changed
	keysChanged     bool
}

// NewLock records the structure of the model in: fields with their types
// and nullability, keys, references and a fingerprint. It reads the model as
// Analyze does, with mml.yaml merged and ref= declarations applied, so a
// reference declared in the sidecar is in the lock and does not make it
// stale. Positions, documentation and annotations are left out, so editing a
// comment does not make the lock stale.
//
// Ensures: entities are sorted by ID and fields keep declaration order, so
// the same input always produces the same lock; in is not modified.
func NewLock(in *Input) Lock {
	entities, _, _ := prepare(in)
	var l Lock
	for _, e := range entities {
		l.entities = append(l.entities, lockEntity(e))
	}
	slices.SortFunc(l.entities, func(a, b lockedEntity) int { return cmp.Compare(a.ID, b.ID) })
	return l
}

func lockEntity(e *Entity) lockedEntity {
	le := lockedEntity{
		ID: e.ID, Name: e.Name, Kind: e.Kind.String(), Source: e.Source.String(),
		Fingerprint: shapeFingerprint(e), PrimaryKey: e.PrimaryKey, Uniques: e.Uniques,
		Fields: make([]lockedField, 0, len(e.Fields)),
	}
	for i := range e.Fields {
		f := &e.Fields[i]
		le.Fields = append(le.Fields, lockedField{
			Name: f.Name, Type: f.Type.String(), Nullable: f.Nullable,
		})
	}
	for _, r := range e.Refs {
		le.Refs = append(le.Refs, lockedRef{Fields: r.Fields, Target: r.Target})
	}
	return le
}

// ReadLock decodes a lock written by WriteLock.
func ReadLock(r io.Reader) (Lock, error) {
	const op = "mixedmodellint.ReadLock"
	var lf lockFile
	if err := json.NewDecoder(r).Decode(&lf); err != nil {
		return Lock{}, &Error{Op: op, Err: &Error{
			Code: EINVALID, Message: fmt.Sprintf("lock is not valid JSON: %v", err),
		}}
	}
	if lf.Version != lockVersion {
		return Lock{}, &Error{Op: op, Err: &Error{
			Code: EINVALID,
			Message: fmt.Sprintf("lock version %d is not supported (want %d); "+
				"regenerate it with mixedmodellint lock", lf.Version, lockVersion),
		}}
	}
	return Lock{entities: lf.Entities}, nil
}

// WriteLock writes l as indented JSON, one field per line, so a change to the
// model shows in review as a small diff.
func WriteLock(w io.Writer, l Lock) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	lf := lockFile{Version: lockVersion, Entities: l.entities}
	if lf.Entities == nil {
		lf.Entities = []lockedEntity{}
	}
	if err := enc.Encode(lf); err != nil {
		return &Error{Op: "mixedmodellint.WriteLock", Err: err}
	}
	return nil
}

// restore returns the locked entities as IR values, for the designs that
// compare the current IR with the previous one.
func (l Lock) restore() []*Entity {
	out := make([]*Entity, 0, len(l.entities))
	for i := range l.entities {
		le := &l.entities[i]
		kind, _ := parseName(kindNames(), le.Kind)
		src, _ := parseName(sourceNames(), le.Source)
		e := &Entity{
			ID: le.ID, Name: le.Name, Canonical: Canonical(le.Name), Kind: kind, Source: src,
			PrimaryKey: le.PrimaryKey, Uniques: le.Uniques, Annotations: Annotations{},
		}
		for _, f := range le.Fields {
			t, _ := parseName(typeNames(), f.Type)
			e.Fields = append(e.Fields, Field{Name: f.Name, Type: t, Nullable: f.Nullable})
		}
		for _, r := range le.Refs {
			e.Refs = append(e.Refs, Ref{Fields: r.Fields, Target: r.Target})
		}
		out = append(out, e)
	}
	return out
}

// shapeFingerprint hashes e's whole structure (mechanism M1): each field's
// name, type, nullability and key role, and every reference. Unlike
// keyFingerprint, which a grain statement records, adding any field changes
// it.
func shapeFingerprint(e *Entity) string {
	parts := make([]string, 0, len(e.Fields)+len(e.Refs))
	for i := range e.Fields {
		f := &e.Fields[i]
		parts = append(parts, fmt.Sprintf("%s:%s:%t:%s",
			strings.ToLower(f.Name), f.Type, f.Nullable, keyRole(e, f.Name)))
	}
	for _, r := range e.Refs {
		parts = append(parts, "ref:"+strings.ToLower(strings.Join(r.Fields, ","))+
			">"+Canonical(lastPart(r.Target)))
	}
	slices.Sort(parts)
	sum := sha256.Sum256([]byte(strings.Join(parts, ";")))
	return hex.EncodeToString(sum[:6])
}

// keyRole is how a field takes part in e's keys: "pk", "unique", or "".
func keyRole(e *Entity, field string) string {
	switch {
	case containsFold(e.PrimaryKey, field):
		return "pk"
	case slices.ContainsFunc(e.Uniques, func(u []string) bool { return containsFold(u, field) }):
		return "unique"
	default:
		return ""
	}
}

// diffLock returns how current differs from locked, entity by entity.
//
// Ensures: entities are matched by ID; an entity whose fingerprint is
// unchanged is left out; changes are sorted by ID.
func diffLock(locked, current []*Entity) []lockChange {
	byID := make(map[string]*Entity, len(locked))
	for _, e := range locked {
		byID[e.ID] = e
	}
	var out []lockChange
	seen := map[string]bool{}
	for _, e := range current {
		seen[e.ID] = true
		old, ok := byID[e.ID]
		switch {
		case !ok:
			out = append(out, lockChange{current: e})
		case shapeFingerprint(old) != shapeFingerprint(e):
			out = append(out, fieldChanges(old, e))
		}
	}
	for _, e := range locked {
		if !seen[e.ID] {
			out = append(out, lockChange{locked: e})
		}
	}
	slices.SortFunc(out, func(a, b lockChange) int { return cmp.Compare(a.id(), b.id()) })
	return out
}

func fieldChanges(old, cur *Entity) lockChange {
	ch := lockChange{
		locked:      old,
		current:     cur,
		keysChanged: keyFingerprint(old) != keyFingerprint(cur),
	}
	for i := range cur.Fields {
		f := &cur.Fields[i]
		of, ok := old.Field(f.Name)
		switch {
		case !ok:
			ch.added = append(ch.added, f.Name)
		case of.Type != f.Type || of.Nullable != f.Nullable:
			ch.retyped = append(ch.retyped, f.Name)
		}
	}
	for i := range old.Fields {
		if _, ok := cur.Field(old.Fields[i].Name); !ok {
			ch.removed = append(ch.removed, old.Fields[i].Name)
		}
	}
	return ch
}

func (ch *lockChange) id() string {
	if ch.current != nil {
		return ch.current.ID
	}
	return ch.locked.ID
}

// describe lists a change's field and key differences for a message.
func (ch *lockChange) describe() string {
	var parts []string
	add := func(label string, names []string) {
		if len(names) > 0 {
			parts = append(parts, label+" "+strings.Join(names, ", "))
		}
	}
	add("added", ch.added)
	add("removed", ch.removed)
	add("retyped", ch.retyped)
	if ch.keysChanged {
		parts = append(parts, "keys or references changed")
	}
	// Every difference in shapeFingerprint is a field added, removed or
	// retyped, or a key or reference changed, so parts is never empty.
	return strings.Join(parts, "; ")
}
