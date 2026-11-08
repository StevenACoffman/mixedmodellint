package mixedmodellint

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

func section1DeviationDesigns() []design {
	return []design{
		{
			id: "1.9-B", title: "Undocumented-shortcut check", strength: Should,
			check: check19B,
		},
	}
}

// check19B reports shortcut markers ("for now", "hack", TODO) in the
// description of an entity or field that has no deviation record stating
// why the shortcut was taken and what it costs (rule_plan.md §1.9-B). The
// record is a deviation annotation naming an ADR with a Rationale and a Cost
// (or Consequences) section; on the entity, it covers every field.
//
// rule_plan.md also accepts an mml.yaml deviations[] entry; until mml.yaml
// has a config section, the sidecar's deviation annotation stands in for it.
// An issue reference alone does not count: it does not put the cost next to
// the entity.
func check19B(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		if outsideConventions(e) {
			continue
		}
		if is, ok := c.undocumentedShortcut(e, nil); ok {
			out = append(out, is)
		}
		for i := range e.Fields {
			if is, ok := c.undocumentedShortcut(e, &e.Fields[i]); ok {
				out = append(out, is)
			}
		}
	}
	return out
}

// undocumentedShortcut applies §1.9-B to e's description, or to f's when f is
// not nil.
func (c *catalog) undocumentedShortcut(e *Entity, f *Field) (issue, bool) {
	doc, where, pos, record := e.Doc, e.Name, e.Pos, ""
	if f != nil {
		doc, where, pos = f.Doc, e.Name+"."+f.Name, f.Pos
		record, _ = f.Annotations.Get("deviation")
	}
	marker := c.rx.shortcut.FindString(doc)
	if marker == "" {
		return issue{}, false
	}
	if record == "" {
		record, _ = e.Annotations.Get("deviation")
	}
	gap, adr := c.deviationGap(record)
	if gap == "" {
		return issue{}, false
	}
	fix := fmt.Sprintf("Annotate %s with deviation=<ADR path> and give that ADR", where)
	if adr != nil {
		fix = "Complete " + adr.Path + " with"
	}
	is := issue{
		entity: e, pos: pos,
		measured: fmt.Sprintf("the description of %s says %q, and %s", where, marker, gap),
		suggestion: fix + " a Rationale section (why the shortcut was taken) and a Cost " +
			"section (what it costs, such as every analytics extract having to unpack it), " +
			"or remove the marker if it is not a modeling shortcut (§1.9).",
	}
	if f != nil {
		is.field = f.Name
	}
	return is, true
}

// deviationGap returns what the deviation record named by ref lacks, or ""
// when it names an ADR whose Rationale and Cost sections both say something,
// and the ADR it names, if any.
func (c *catalog) deviationGap(ref string) (string, *Document) {
	if ref == "" {
		return "no deviation record states its rationale and cost", nil
	}
	adr, ok := c.adrAt(ref)
	if !ok {
		return fmt.Sprintf("its deviation record %q names no ADR in docs/adr/", ref), nil
	}
	var missing []string
	if !c.sectionFilled(adr.Text, c.rx.adrRationale) {
		missing = append(missing, "Rationale")
	}
	if !c.sectionFilled(adr.Text, c.rx.adrCost) {
		missing = append(missing, "Cost")
	}
	if len(missing) == 0 {
		return "", adr
	}
	return fmt.Sprintf("its ADR %s has no filled-in %s section", adr.Path,
		orList(missing)), adr
}

// adrAt returns the ADR a deviation annotation names: by its path, or by its
// file name with or without the extension ("0007-catalog-json").
func (c *catalog) adrAt(ref string) (*Document, bool) {
	ref = strings.TrimSpace(ref)
	for i := range c.artifacts.ADRs {
		d := &c.artifacts.ADRs[i]
		base := path.Base(d.Path)
		if d.Path == ref || strings.HasSuffix(d.Path, "/"+ref) ||
			strings.EqualFold(strings.TrimSuffix(base, path.Ext(base)), ref) {
			return d, true
		}
	}
	return nil, false
}

// sectionFilled reports whether text has a Markdown section whose heading
// matches heading and which has at least one line that records something,
// rather than being empty or TODO.
func (c *catalog) sectionFilled(text string, heading *regexp.Regexp) bool {
	in := false
	for line := range strings.Lines(text) {
		switch {
		case heading.MatchString(line):
			in = true
		case strings.HasPrefix(strings.TrimSpace(line), "#"):
			in = false
		case in && !c.rx.emptySection.MatchString(line):
			return true
		}
	}
	return false
}
