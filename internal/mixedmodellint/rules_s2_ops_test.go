package mixedmodellint_test

import (
	"testing"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

func TestAgentPermissionsAsData(t *testing.T) {
	t.Parallel()
	status := func() m.Field {
		return enumField("status", []string{"open", "pending", "closed"})
	}
	tickets := func(kv []string, grants ...m.Grant) m.Entity {
		e := entityOf("tickets", kv, col("ticket_id", m.TypeInt), status())
		e.PrimaryKey = []string{"ticket_id"}
		e.Grants = grants
		return e
	}
	grant := func(role string, privs ...string) m.Grant {
		return m.Grant{Role: role, Privileges: privs}
	}
	writes := []string{"agent_ops", "read,write"}
	transitions := table("ticket_status_transitions", []string{"from_state", "to_state"},
		col("from_state", m.TypeString), col("to_state", m.TypeString))
	cases := map[string]struct {
		entities []m.Entity
		want     []string
	}{
		"agents write a status with no transitions": {
			entities: []m.Entity{tickets(writes)},
			want:     []string{"2.8-A tickets"},
		},
		"the field declares its transitions": {
			entities: []m.Entity{func() m.Entity {
				e := tickets(writes)
				e.Fields[1].Annotations = ann("transitions", "open: [pending, closed]")
				return e
			}()},
		},
		"a transitions table for the field": {
			entities: []m.Entity{tickets(writes), transitions},
		},
		"a generic transitions table referencing the entity": {
			entities: []m.Entity{
				tickets(writes),
				referencing(new(table("state_transitions", nil, col("ticket_id", m.TypeInt),
					col("from_state", m.TypeString), col("to_state", m.TypeString))),
					"ticket_id", "tickets"),
			},
		},
		"agents only read": {
			entities: []m.Entity{tickets([]string{"agent_ops", "read"})},
		},
		"an agent role is granted writes but no operations are declared": {
			entities: []m.Entity{tickets(nil, grant("support_agent", "INSERT")), transitions},
			want:     []string{"2.8-A tickets"},
		},
		"the declaration omits write but an agent may update": {
			entities: []m.Entity{
				tickets([]string{"agent_ops", "read"}, grant("llm_writer", "SELECT", "UPDATE")),
				transitions,
			},
			want: []string{"2.8-A tickets"},
		},
		"the declaration omits delete but an agent may delete": {
			entities: []m.Entity{tickets(writes, grant("bot", "DELETE")), transitions},
			want:     []string{"2.8-A tickets"},
		},
		"declared and granted alike": {
			entities: []m.Entity{
				tickets(
					[]string{"agent_ops", "read,write,delete"},
					grant("bot", "UPDATE", "DELETE"),
				),
				transitions,
			},
		},
		"a role that is not an agent": {
			entities: []m.Entity{tickets(nil, grant("app", "INSERT", "UPDATE", "DELETE"))},
		},
		"exposed to agents": {
			entities: []m.Entity{tickets([]string{"agent_exposed", "true"})},
			want:     []string{"2.8-A tickets"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.want, findingsOf(src(tc.entities...), "2.8-A"))
		})
	}
}
