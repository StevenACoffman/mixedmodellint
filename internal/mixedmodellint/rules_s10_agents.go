package mixedmodellint

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

func section10AgentDesigns() []design {
	return []design{
		{id: "10.6-A", title: "Agent-action shape check", strength: Must, check: check106A},
		{id: "10.7-A", title: "Artifact-entity graph check", strength: Should, check: check107A},
		{id: "10.9-A", title: "Synthetic-data marker check", strength: Must, check: check109A},
	}
}

// check106A reports entities recording agent actions that do not keep what
// each action was based on: the prompt, the tools available and the model,
// all required, so an action can be replayed and debugged (§10.6). A
// prompt_ref (a URI and hash) stands in for the prompt where privacy rules
// keep prompts out of the row.
func check106A(c *catalog) []issue {
	context := []struct {
		what  string
		found func(*Field) bool
	}{
		{
			"the prompt (prompt, input_context, messages or prompt_ref)",
			namedLike(c.rx.contextPrompt),
		},
		{"the tools available (tools_available or tool_manifest)", namedLike(c.rx.contextTools)},
		{"the model (model_version, model_id or model)", namedLike(c.rx.contextModel)},
	}
	var out []issue
	for _, e := range c.entities {
		if !c.agentAction(e) {
			continue
		}
		var missing []string
		for _, need := range context {
			if gap := requiredField(e, need.what, need.found); gap != "" {
				missing = append(missing, gap)
			}
		}
		if len(missing) > 0 {
			out = append(out, issue{
				entity: e,
				measured: fmt.Sprintf("%s records agent actions without %s", e.Name,
					strings.Join(missing, "; ")),
				suggestion: "Add prompt jsonb NOT NULL (or prompt_ref text NOT NULL), " +
					"tools_available jsonb NOT NULL and model_version text NOT NULL, so each " +
					"action can be replayed and debugged (§10.6).",
			})
		}
	}
	return out
}

// namedLike returns a test of a field's name, in snake_case, against rx.
func namedLike(rx *regexp.Regexp) func(*Field) bool {
	return func(f *Field) bool { return rx.MatchString(snakeName(f.Name)) }
}

// requiredField says what is wrong with the field of e that found accepts,
// described as what: missing, or allowed to be absent. It returns "" when e
// has it and requires it.
func requiredField(e *Entity, what string, found func(*Field) bool) string {
	i := slices.IndexFunc(e.Fields, func(f Field) bool { return found(&f) })
	switch {
	case i < 0:
		return what
	case e.Fields[i].Nullable:
		return "a required " + e.Fields[i].Name + " (it may be missing)"
	default:
		return ""
	}
}

// check107A reports a model that trains models or runs agents without
// modeling the artifacts as data (§10.7): with ML signals (an entity in the
// training or feature layer), a model_version linked to its dataset, its
// training run and its features, with its hyperparameters; with agent
// signals (an entity recording agent actions), an agent_run and a tool_call
// linked to it, with the call's name, arguments, result and time.
// rule_plan.md also reads mlflow and wandb configs, model files and SDK
// imports as signals; those need artifact loaders, so only the model's own
// entities are signals here.
func check107A(c *catalog) []issue {
	var out []issue
	if signal := c.firstEntity(isMLData); signal != nil {
		if is, ok := c.mlArtifactGaps(signal); ok {
			out = append(out, is)
		}
	}
	if signal := c.firstEntity(c.agentAction); signal != nil {
		if is, ok := c.agentArtifactGaps(signal); ok {
			out = append(out, is)
		}
	}
	return out
}

// isMLData reports whether e is training or feature data, the ML signal.
func isMLData(e *Entity) bool {
	layer, _ := e.Annotations.Get("layer")
	return layer == "training" || layer == "feature"
}

// firstEntity returns the first entity that matches, in catalog order, or nil.
func (c *catalog) firstEntity(match func(*Entity) bool) *Entity {
	for _, e := range c.entities {
		if match(e) {
			return e
		}
	}
	return nil
}

// mlArtifactGaps applies §10.7-A's ML half, reporting on the model_version
// entity when there is one and otherwise on the signal.
func (c *catalog) mlArtifactGaps(signal *Entity) (issue, bool) {
	mv := c.firstEntity(func(e *Entity) bool { return roleName(e) == "model_version" })
	if mv == nil {
		return artifactIssue(signal, signal.Name+" is training or feature data",
			[]string{"a model_version entity"},
			"model_versions(model_version_id, dataset_id REFERENCES training_datasets, "+
				"run_id REFERENCES training_runs, feature_set_id REFERENCES feature_sets, "+
				"hyperparameters jsonb)"), true
	}
	var gaps []string
	for _, link := range []string{"dataset", "run", "feature"} {
		if !slices.ContainsFunc(c.parents(mv), func(p *Entity) bool { return hasWord(p, link) }) {
			gaps = append(gaps, "a reference from model_version to its "+link+" entity")
		}
	}
	if _, ok := mv.Field("hyperparameters"); !ok {
		gaps = append(gaps, "the hyperparameters of model_version")
	}
	if len(gaps) == 0 {
		return issue{}, false
	}
	return artifactIssue(mv, signal.Name+" is training or feature data", gaps,
		"the missing columns and references"), true
}

// agentArtifactGaps applies §10.7-A's agent half, reporting on the tool_call
// entity, else the agent_run entity, else the signal.
func (c *catalog) agentArtifactGaps(signal *Entity) (issue, bool) {
	run := c.firstEntity(func(e *Entity) bool { return roleName(e) == "agent_run" })
	call := c.firstEntity(func(e *Entity) bool { return roleName(e) == "tool_call" })
	var gaps []string
	if run == nil {
		gaps = append(gaps, "an agent_run entity")
	}
	if call == nil {
		gaps = append(gaps, "a tool_call entity")
	} else {
		gaps = append(gaps, toolCallGaps(c, call, run)...)
	}
	if len(gaps) == 0 {
		return issue{}, false
	}
	at := signal
	for _, e := range []*Entity{run, call} {
		if e != nil {
			at = e
		}
	}
	return artifactIssue(at, signal.Name+" records agent actions", gaps,
		"agent_runs(agent_run_id PRIMARY KEY, prompt, tools_available, model_version) and "+
			"tool_calls(call_id PRIMARY KEY, agent_run_id REFERENCES agent_runs, tool_name, "+
			"arguments jsonb, result jsonb, called_at timestamptz)"), true
}

// toolCallGaps lists what tool_call entity call lacks: a key, a reference to
// the agent run, and the call's name, arguments, result and time.
func toolCallGaps(c *catalog, call, run *Entity) []string {
	var gaps []string
	if len(call.PrimaryKey) == 0 {
		gaps = append(gaps, "a key on tool_call")
	}
	if run != nil && !slices.Contains(c.parents(call), run) {
		gaps = append(gaps, "a reference from tool_call to agent_run")
	}
	for _, name := range []string{"tool_name", "arguments", "result", "called_at"} {
		if _, ok := call.Field(name); !ok {
			gaps = append(gaps, "tool_call."+name)
		}
	}
	return gaps
}

// artifactIssue reports gaps on e; signal says why the artifacts are needed.
func artifactIssue(e *Entity, signal string, gaps []string, add string) issue {
	return issue{
		entity: e,
		measured: fmt.Sprintf("%s, but the schema lacks %s", signal,
			strings.Join(gaps, ", ")),
		suggestion: fmt.Sprintf("Model the artifacts as data: add %s (§10.7).", add),
	}
}

// roleName is e's name in singular snake_case, with no affix dropped: the
// name §10.7's role entities are found by. Canonical would turn
// model_versions into version, since Model is an affix in OrderModel.
func roleName(e *Entity) string {
	words := splitWords(e.Name)
	for i, w := range words {
		words[i] = singular(w)
	}
	return strings.Join(words, "_")
}

// hasWord reports whether e's role name has w as one of its words, so
// training_runs has "run" and prune_log does not.
func hasWord(e *Entity, w string) bool {
	return slices.Contains(strings.Split(roleName(e), "_"), w)
}

// check109A reports entities that may hold synthetic records without the
// markers that tell them from real ones (§10.9): is_synthetic, required;
// generator_version, required by a CHECK when is_synthetic is true (in SQL,
// where a CHECK can say so); and source_dataset_id, referencing the dataset
// entity when the model has one. A missing quality signal is only a note, as
// the rule's own example names just the three markers.
func check109A(c *catalog) []issue {
	var out []issue
	for _, e := range c.entities {
		if !maySynthesize(e) {
			continue
		}
		if gaps := c.syntheticMarkerGaps(e); len(gaps) > 0 {
			out = append(out, issue{
				entity: e,
				measured: fmt.Sprintf("%s may contain synthetic records but has no %s", e.Name,
					strings.Join(gaps, ", no ")),
				suggestion: "Add is_synthetic boolean NOT NULL DEFAULT false, generator_version " +
					"text, source_dataset_id NOT NULL (referencing the dataset), and CHECK (NOT " +
					"is_synthetic OR generator_version IS NOT NULL) (§10.9).",
			})
			continue
		}
		if !c.recordsQuality(e) {
			out = append(out, issue{
				entity: e, level: SeverityNote,
				measured: fmt.Sprintf("%s marks synthetic records but records nothing about "+
					"their quality", e.Name),
				suggestion: fmt.Sprintf("Add %s_quality_score, or point quality= at the test "+
					"or validation that checks the generated data (§10.9).", e.Canonical),
			})
		}
	}
	return out
}

// maySynthesize reports whether e may hold synthetic records: declared
// synthetic=possible (or only), or training or feature data not declared
// synthetic=never.
func maySynthesize(e *Entity) bool {
	switch v, _ := e.Annotations.Get("synthetic"); v {
	case "never":
		return false
	case "possible", "only":
		return true
	default:
		return isMLData(e)
	}
}

// syntheticMarkerGaps lists the markers e lacks.
func (c *catalog) syntheticMarkerGaps(e *Entity) []string {
	var gaps []string
	if f, ok := e.Field("is_synthetic"); !ok || f.Type != TypeBool || f.Nullable {
		gaps = append(gaps, "is_synthetic boolean NOT NULL")
	}
	if _, ok := e.Field("generator_version"); !ok {
		gaps = append(gaps, "generator_version")
	}
	if gap := c.sourceDatasetGap(e); gap != "" {
		gaps = append(gaps, gap)
	}
	if e.Source == SourceSQL && !tiedByCheck(e, "is_synthetic", "generator_version") {
		gaps = append(gaps, "CHECK tying generator_version to is_synthetic")
	}
	return gaps
}

// sourceDatasetGap says what e lacks to name the dataset its records come
// from: the source_dataset_id column, or, when the model has a dataset
// entity, a reference to it. It returns "" when nothing is missing.
func (c *catalog) sourceDatasetGap(e *Entity) string {
	f, ok := e.Field("source_dataset_id")
	if !ok {
		return "source_dataset_id"
	}
	d := c.firstEntity(func(t *Entity) bool { return t != e && hasWord(t, "dataset") })
	if d != nil && c.referenced(e, f) != d {
		return "reference from source_dataset_id to " + d.Name
	}
	return ""
}

// tiedByCheck reports whether a CHECK on e reads both columns a and b.
func tiedByCheck(e *Entity, a, b string) bool {
	return slices.ContainsFunc(e.Checks, func(ch Check) bool {
		return containsFold(ch.Columns, a) && containsFold(ch.Columns, b)
	})
}

// recordsQuality reports whether e says how good its generated data is: a
// quality, fidelity or validation score or status, or a quality= reference.
func (c *catalog) recordsQuality(e *Entity) bool {
	return e.Annotations.Has("quality") ||
		slices.ContainsFunc(
			e.Fields,
			func(f Field) bool { return c.rx.qualityField.MatchString(f.Name) },
		)
}
