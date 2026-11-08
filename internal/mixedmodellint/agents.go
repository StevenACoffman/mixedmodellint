package mixedmodellint

import (
	"regexp"
	"slices"
	"strings"
)

// agentLexicon holds the names by which the agent rules (§2.8, §10.6, §10.7)
// recognize AI agents in a model: the roles they connect as, the entities
// recording what they did, and the context each action must keep. rule_plan.md makes the role list
// an
// agents.roles setting; until mml.yaml has a config section, a role is an
// agent's when its name says so.
type agentLexicon struct {
	agentRole   *regexp.Regexp // 2.8-A: database roles agents connect as
	statusField *regexp.Regexp // 2.8-A: fields holding a life-cycle state

	agentActionName *regexp.Regexp // 10.6-A: entities recording what agents did
	agentActor      *regexp.Regexp // 10.6-A: actor_type values meaning an agent
	contextPrompt   *regexp.Regexp // 10.6-A: the input an agent acted on
	contextTools    *regexp.Regexp // 10.6-A: the tools it could call
	contextModel    *regexp.Regexp // 10.6-A: the model that acted
}

func newAgentLexicon() agentLexicon {
	return agentLexicon{
		agentRole:   regexp.MustCompile(`(?i)(^|_)(agents?|llm|ai|assistant|copilot|bot)(_|$)`),
		statusField: regexp.MustCompile(`(?i)(^|_)(status|state|stage|phase)$`),
		agentActionName: regexp.MustCompile(`(?i)(^|_)(agent|llm|assistant|copilot|bot)_?` +
			`(action|step|call|decision|run)s?$`),
		agentActor: regexp.MustCompile(`(?i)^(agent|ai|llm|bot)$`),
		contextPrompt: regexp.MustCompile(
			`(?i)^(prompt|prompt_ref|input_context|messages|system_prompt)$`,
		),
		contextTools: regexp.MustCompile(`(?i)^(tools_available|tool_manifest|tools)$`),
		contextModel: regexp.MustCompile(`(?i)^(model_version|model_id|model)$`),
	}
}

// agentGrants returns the grants e gives to agent roles.
func (c *catalog) agentGrants(e *Entity) []Grant {
	var out []Grant
	for _, g := range e.Grants {
		if c.rx.agentRole.MatchString(g.Role) {
			out = append(out, g)
		}
	}
	return out
}

// agentOps returns the operations e's agent_ops declares, lower case, and
// whether it declares any.
func agentOps(e *Entity) ([]string, bool) {
	v, ok := e.Annotations.Get("agent_ops")
	if !ok {
		return nil, false
	}
	return splitList(strings.ToLower(v)), true
}

// agentMayWrite reports whether ops lets agents do more than read.
func agentMayWrite(ops []string) bool {
	return slices.ContainsFunc(
		ops,
		func(op string) bool { return op != "read" && op != "aggregate" },
	)
}

// agentAction reports whether e records actions agents take: named so
// (agent_actions, llm_calls), declared actor=agent, or with an actor_type
// whose values include an agent.
func (c *catalog) agentAction(e *Entity) bool {
	if actor, _ := e.Annotations.Get("actor"); actor == "agent" ||
		c.rx.agentActionName.MatchString(snakeName(e.Name)) {
		return true
	}
	if f, ok := e.Field("actor_type"); ok {
		return slices.ContainsFunc(enumValues(e, f), c.rx.agentActor.MatchString)
	}
	return false
}
