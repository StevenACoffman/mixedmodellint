package mixedmodellint_test

import (
	"testing"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// nullable returns a copy of f allowing NULL.
func nullable(f *m.Field) m.Field {
	out := *f
	out.Nullable = true
	return out
}

func TestAgentActionContext(t *testing.T) {
	t.Parallel()
	context := []m.Field{
		col("prompt", m.TypeJSON), col("tools_available", m.TypeJSON),
		col("model_version", m.TypeString),
	}
	actions := func(fields ...m.Field) m.Entity {
		return table("agent_actions", []string{"action_id"},
			append([]m.Field{col("action_id", m.TypeInt)}, fields...)...)
	}
	cases := map[string]struct {
		entity m.Entity
		want   []string
	}{
		"no context at all": {entity: actions(), want: []string{"10.6-A agent_actions"}},
		"the full context":  {entity: actions(context...)},
		"a prompt kept by reference": {
			entity: actions(col("prompt_ref", m.TypeString), context[1], context[2]),
		},
		"a nullable model": {
			entity: actions(context[0], context[1], nullable(&context[2])),
			want:   []string{"10.6-A agent_actions"},
		},
		"an event whose actor may be an agent": {
			entity: table("ticket_events", []string{"event_id"}, col("event_id", m.TypeInt),
				enumField("actor_type", []string{"user", "agent"})),
			want: []string{"10.6-A ticket_events"},
		},
		"declared an agent's actions": {
			entity: entityOf(
				"decisions",
				[]string{"actor", "agent"},
				col("decision_id", m.TypeInt),
			),
			want: []string{"10.6-A decisions"},
		},
		"people's actions": {
			entity: table("ticket_events", []string{"event_id"}, col("event_id", m.TypeInt),
				enumField("actor_type", []string{"user", "admin"})),
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.want, findingsOf(src(tc.entity), "10.6-A"))
		})
	}
}

func TestArtifactEntities(t *testing.T) {
	t.Parallel()
	features := entityOf("customer_features", []string{"layer", "feature"},
		col("customer_id", m.TypeInt))
	dataset := table("training_datasets", []string{"dataset_id"}, col("dataset_id", m.TypeInt))
	run := table("training_runs", []string{"run_id"}, col("run_id", m.TypeInt))
	featureSet := table(
		"feature_sets",
		[]string{"feature_set_id"},
		col("feature_set_id", m.TypeInt),
	)
	modelVersion := func(fields ...m.Field) m.Entity {
		e := table("model_versions", []string{"model_version_id"},
			append([]m.Field{
				col("model_version_id", m.TypeInt), col("dataset_id", m.TypeInt),
				col("run_id", m.TypeInt), col("feature_set_id", m.TypeInt),
			}, fields...)...)
		e.Refs = []m.Ref{
			{Fields: []string{"dataset_id"}, Target: "training_datasets"},
			{Fields: []string{"run_id"}, Target: "training_runs"},
			{Fields: []string{"feature_set_id"}, Target: "feature_sets"},
		}
		return e
	}
	actions := table("agent_actions", []string{"action_id"}, col("action_id", m.TypeInt))
	agentRun := table("agent_runs", []string{"agent_run_id"}, col("agent_run_id", m.TypeInt))
	toolCall := func(fields ...m.Field) m.Entity {
		return referencing(new(table("tool_calls", []string{"call_id"},
			append([]m.Field{col("call_id", m.TypeInt), col("agent_run_id", m.TypeInt)},
				fields...)...)), "agent_run_id", "agent_runs")
	}
	callFields := []m.Field{
		col("tool_name", m.TypeString), col("arguments", m.TypeJSON),
		col("result", m.TypeJSON), col("called_at", m.TypeTimestamptz),
	}
	cases := map[string]struct {
		entities []m.Entity
		want     []string
	}{
		"features but no model versions": {
			entities: []m.Entity{features},
			want:     []string{"10.7-A customer_features"},
		},
		"model versions with their lineage": {
			entities: []m.Entity{
				features, dataset, run, featureSet,
				modelVersion(col("hyperparameters", m.TypeJSON)),
			},
		},
		"model versions without hyperparameters": {
			entities: []m.Entity{features, dataset, run, featureSet, modelVersion()},
			want:     []string{"10.7-A model_versions"},
		},
		"agents but no runs or tool calls": {
			entities: []m.Entity{actions},
			want:     []string{"10.7-A agent_actions"},
		},
		"agents with runs and tool calls": {
			entities: []m.Entity{actions, agentRun, toolCall(callFields...)},
		},
		"tool calls without their results": {
			entities: []m.Entity{actions, agentRun, toolCall(callFields[:2]...)},
			want:     []string{"10.7-A tool_calls"},
		},
		"no ML or agents": {
			entities: []m.Entity{table("orders", []string{"order_id"}, col("order_id", m.TypeInt))},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.want, findingsOf(src(tc.entities...), "10.7-A"))
		})
	}
}

func TestSyntheticMarkers(t *testing.T) {
	t.Parallel()
	markers := []m.Field{
		col("is_synthetic", m.TypeBool), nullable(new(col("generator_version", m.TypeString))),
		col("source_dataset_id", m.TypeInt), col("quality_score", m.TypeDecimal),
	}
	tied := m.Check{Columns: []string{"is_synthetic", "generator_version"}}
	examples := func(kv []string, checks []m.Check, fields ...m.Field) m.Entity {
		e := entityOf("training_examples", kv,
			append([]m.Field{col("example_id", m.TypeInt)}, fields...)...)
		e.PrimaryKey = []string{"example_id"}
		e.Checks = checks
		return e
	}
	training := []string{"layer", "training"}
	cases := map[string]struct {
		entities []m.Entity
		want     []string
		severity m.Severity
	}{
		"training data with no markers": {
			entities: []m.Entity{examples(training, nil)},
			want:     []string{"10.9-A training_examples"},
			severity: m.SeverityError,
		},
		"every marker and the check": {
			entities: []m.Entity{examples(training, []m.Check{tied}, markers...)},
		},
		"markers without the check": {
			entities: []m.Entity{examples(training, nil, markers...)},
			want:     []string{"10.9-A training_examples"},
			severity: m.SeverityError,
		},
		"a nullable marker": {
			entities: []m.Entity{examples(training, []m.Check{tied},
				nullable(&markers[0]), markers[1], markers[2], markers[3])},
			want:     []string{"10.9-A training_examples"},
			severity: m.SeverityError,
		},
		"no quality signal is a note": {
			entities: []m.Entity{examples(training, []m.Check{tied}, markers[:3]...)},
			want:     []string{"10.9-A training_examples"},
			severity: m.SeverityNote,
		},
		"declared never synthetic": {
			entities: []m.Entity{
				examples([]string{"layer", "training", "synthetic", "never"}, nil),
			},
		},
		"an OLTP table declared to receive synthetic rows": {
			entities: []m.Entity{examples([]string{"synthetic", "possible"}, nil)},
			want:     []string{"10.9-A training_examples"},
			severity: m.SeverityError,
		},
		"an OLTP table by default": {entities: []m.Entity{examples(nil, nil)}},
		"a dataset table the source must reference": {
			entities: []m.Entity{
				table("datasets", []string{"dataset_id"}, col("dataset_id", m.TypeInt)),
				examples(training, []m.Check{tied}, markers...),
			},
			want:     []string{"10.9-A training_examples"},
			severity: m.SeverityError,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := src(tc.entities...)
			equals(t, tc.want, findingsOf(in, "10.9-A"))
			for _, f := range m.Analyze(in).Findings {
				if f.Design == "10.9-A" {
					equals(t, tc.severity, f.Severity)
				}
			}
		})
	}
}
