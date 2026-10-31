package mixedmodellint

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"
)

// grainInfo is an entity's declared grain, parsed: the statement text, its
// nouns ("one row per product per sale date" has product and sale date), the
// columns those nouns map to, and the nouns no column matched.
type grainInfo struct {
	text       string
	fp         string
	nouns      []string
	columns    []string
	unresolved []string
}

// grainOf parses e's grain annotation. ok is false when e declares none.
func grainOf(e *Entity) (g grainInfo, ok bool) {
	raw, ok := e.Annotations.Get("grain")
	if !ok {
		return grainInfo{}, false
	}
	text, params := splitParams(raw)
	g = grainInfo{text: text, fp: params["fp"], nouns: grainNouns(text)}
	for _, n := range g.nouns {
		cols := nounColumns(e, n)
		if len(cols) == 0 {
			g.unresolved = append(g.unresolved, n)
			continue
		}
		g.columns = append(g.columns, cols...)
	}
	g.columns = dedupFold(g.columns)
	return g, true
}

// grainNouns returns the noun phrases after each "per": "one row per order
// line (UTC)" has one noun, "order line". A trailing parenthesized qualifier
// is not a noun.
func grainNouns(text string) []string {
	if i := strings.IndexByte(text, '('); i >= 0 {
		text = text[:i]
	}
	parts := strings.Split(strings.ToLower(text), " per ")
	var nouns []string
	for _, p := range parts[1:] {
		if n := strings.TrimSpace(p); n != "" {
			nouns = append(nouns, n)
		}
	}
	return nouns
}

// nounColumns maps one grain noun to the columns of e that identify it:
//   - the entity's own name ("order line" on order_lines) maps to its primary
//     key, or to its <entity>_id column when it has no key;
//   - a referenced entity's name maps to the referencing columns (customer →
//     customer_id);
//   - a noun that is itself a column name maps to that column (sale date →
//     sale_date), as does "<noun>_id";
//   - a bare time unit (day, hour, month, ...) maps to the one date or
//     timestamp column in the key, when there is exactly one.
//
// It returns nil when nothing matches.
func nounColumns(e *Entity, noun string) []string {
	canon := Canonical(noun)
	if canon == e.Canonical || canon == Canonical(e.Name) {
		if len(e.PrimaryKey) > 0 {
			return e.PrimaryKey
		}
		// No key: the row's own identifier column, if it has one, so 7.1-B
		// can say the grain is unverifiable rather than unresolvable.
		return namedColumns(e, canon)
	}
	if cols := namedColumns(e, canon); cols != nil {
		return cols
	}
	if timeUnitRank(noun) > 0 || strings.HasSuffix(noun, " date") || noun == "date" {
		return keyTimeColumn(e)
	}
	return nil
}

// namedColumns returns the columns of the reference to the entity named
// canon, or the one column named canon or canon_id, or nil.
func namedColumns(e *Entity, canon string) []string {
	for _, r := range e.Refs {
		if Canonical(lastPart(r.Target)) == canon {
			return r.Fields
		}
	}
	for i := range e.Fields {
		fc := Canonical(e.Fields[i].Name)
		if fc == canon || fc == canon+"_id" {
			return []string{e.Fields[i].Name}
		}
	}
	return nil
}

// keyTimeColumn returns the single temporal column that is part of e's
// primary key, or nil when there is none or more than one.
func keyTimeColumn(e *Entity) []string {
	var found []string
	for _, k := range e.PrimaryKey {
		if f, ok := e.Field(k); ok && isTemporal(f.Type) {
			found = append(found, f.Name)
		}
	}
	if len(found) != 1 {
		return nil
	}
	return found
}

func isTemporal(t LogicalType) bool {
	return t == TypeDate || t == TypeTimestamptz || t == TypeTimestampNaive
}

// inferGrain proposes a grain statement from e's primary key (mechanism M2).
// A key made only of references and dates names each of them: PK
// (product_id, sale_date) becomes "one row per product per sale date". A key
// with any other column, a surrogate id or a line number within a parent,
// identifies the entity's own rows: PK (order_id, line_no) on order_lines
// becomes "one row per order line". It returns "" for an entity with no
// primary key.
func inferGrain(e *Entity) string {
	if len(e.PrimaryKey) == 0 {
		return ""
	}
	own := "one row per " + strings.ReplaceAll(Canonical(e.Name), "_", " ")
	var nouns []string
	for _, k := range e.PrimaryKey {
		noun, ok := keyNoun(e, k)
		if !ok {
			return own
		}
		nouns = append(nouns, noun)
	}
	return "one row per " + strings.Join(slices.Compact(nouns), " per ")
}

// keyNoun names the thing a key column identifies when it is a reference
// (customer_id → customer) or a date (sale_date → sale date). ok is false for
// any other column.
func keyNoun(e *Entity, col string) (string, bool) {
	for _, r := range e.Refs {
		if len(r.Fields) == 1 && strings.EqualFold(r.Fields[0], col) {
			return strings.ReplaceAll(Canonical(lastPart(r.Target)), "_", " "), true
		}
	}
	if f, ok := e.Field(col); ok && isTemporal(f.Type) {
		return strings.ReplaceAll(Canonical(col), "_", " "), true
	}
	return "", false
}

// keyFingerprint hashes e's key structure: every PK and UNIQUE column set and
// every FK target (mechanism M1). A grain statement records it as fp=, and a
// later key change makes the statement stale. Adding a non-key column does not
// change it.
func keyFingerprint(e *Entity) string {
	var parts []string
	for _, k := range e.Keys() {
		cols := make([]string, len(k))
		for i, c := range k {
			cols[i] = strings.ToLower(c)
		}
		slices.Sort(cols)
		parts = append(parts, strings.Join(cols, ","))
	}
	for _, r := range e.Refs {
		parts = append(parts, "ref:"+strings.ToLower(strings.Join(r.Fields, ","))+
			">"+Canonical(lastPart(r.Target)))
	}
	slices.Sort(parts)
	sum := sha256.Sum256([]byte(strings.Join(parts, ";")))
	return hex.EncodeToString(sum[:4])
}

// sameColumns reports whether a and b hold the same column names, ignoring
// order and case.
func sameColumns(a, b []string) bool {
	return slices.Equal(foldSorted(a), foldSorted(b))
}

func foldSorted(cols []string) []string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = strings.ToLower(c)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func dedupFold(cols []string) []string {
	var out []string
	for _, c := range cols {
		if !slices.ContainsFunc(out, func(o string) bool { return strings.EqualFold(o, c) }) {
			out = append(out, c)
		}
	}
	return out
}

// lastPart returns the part of a qualified name after the last '.', so
// "public.orders" links as "orders".
func lastPart(name string) string {
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		return name[i+1:]
	}
	return name
}
