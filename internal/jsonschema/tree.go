package jsonschema

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// node is a decoded JSON value that remembers where it started, because
// encoding/json alone gives no positions and a finding needs a line.
type node struct {
	off     int
	members []member // object, in source order
	items   []*node  // array
	value   any      // string, float64, bool or nil for a scalar
	isObj   bool
	isArr   bool
}

type member struct {
	key    string
	keyOff int
	val    *node
}

// treeParser builds a node tree from a json.Decoder's tokens.
type treeParser struct {
	dec *json.Decoder
	src []byte
}

// get returns the member named key, or nil.
func (n *node) get(key string) *node {
	if n == nil {
		return nil
	}
	for i := range n.members {
		if n.members[i].key == key {
			return n.members[i].val
		}
	}
	return nil
}

// parseTree decodes src into a node tree with byte offsets.
func parseTree(src []byte) (*node, error) {
	dec := json.NewDecoder(bytes.NewReader(src))
	dec.UseNumber()
	p := &treeParser{dec: dec, src: src}
	root, err := p.value()
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("trailing data after the top-level value")
	}
	return root, nil
}

// next reads a token and returns it with the offset where it starts: the
// decoder reports the offset after the previous token, so skip the
// whitespace and separators between.
func (p *treeParser) next() (json.Token, int, error) {
	off := int(p.dec.InputOffset())
	for off < len(p.src) && bytes.IndexByte([]byte(" \t\r\n,:"), p.src[off]) >= 0 {
		off++
	}
	tok, err := p.dec.Token()
	if err != nil {
		return nil, off, fmt.Errorf("read json token: %w", err)
	}
	return tok, off, nil
}

func (p *treeParser) value() (*node, error) {
	tok, off, err := p.next()
	if err != nil {
		return nil, err
	}
	switch tok {
	case json.Delim('{'):
		return p.object(off)
	case json.Delim('['):
		return p.array(off)
	default:
		if num, ok := tok.(json.Number); ok {
			f, _ := num.Float64()
			return &node{off: off, value: f}, nil
		}
		return &node{off: off, value: tok}, nil
	}
}

func (p *treeParser) object(off int) (*node, error) {
	n := &node{off: off, isObj: true}
	for p.dec.More() {
		tok, keyOff, err := p.next()
		if err != nil {
			return nil, err
		}
		key, _ := tok.(string)
		val, err := p.value()
		if err != nil {
			return nil, err
		}
		n.members = append(n.members, member{key: key, keyOff: keyOff, val: val})
	}
	if _, _, err := p.next(); err != nil { // the closing brace
		return nil, err
	}
	return n, nil
}

func (p *treeParser) array(off int) (*node, error) {
	n := &node{off: off, isArr: true}
	for p.dec.More() {
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		n.items = append(n.items, v)
	}
	if _, _, err := p.next(); err != nil { // the closing bracket
		return nil, err
	}
	return n, nil
}
