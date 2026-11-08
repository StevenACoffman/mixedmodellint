package mixedmodellint_test

import (
	"testing"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// s10MessageCases covers the §10 designs: one input per message variant.
func s10MessageCases(t *testing.T) map[string]messageCase {
	t.Helper()
	audited := placed("customers.sql", new(table("customers", []string{"customer_id"},
		col("customer_id", m.TypeInt), col("updated_at", m.TypeTimestamptz),
		fieldWith("_loaded_at", m.TypeTimestamptz, "time", "system"))))
	events := placed("events.sql", new(table("events", []string{"event_id"},
		col("event_id", m.TypeInt), col("payload", m.TypeJSON))))
	orderSchema := placed("order.schema.json", &m.Entity{
		ID: "json:order.schema.json#", Kind: m.KindSchema, Source: m.SourceJSONSchema,
		Name: "Order", Annotations: ann("extension", "attributes"),
		Fields: []m.Field{
			col("order_id", m.TypeInt),
			{Name: "category", RawType: "array|object|string"},
		},
	})
	orders := placed("orders.sql", new(table("orders", []string{"order_id"},
		col("order_id", m.TypeInt), col("status", m.TypeString))))
	docs := placed("documents.sql", new(table("documents", []string{"document_id"},
		col("document_id", m.TypeUUID), col("body", m.TypeBytes), col("file_uri", m.TypeString),
		col("uploaded_at", m.TypeTimestamptz))))
	reviews := placed("reviews.sql", new(table("reviews", []string{"review_id"},
		col("review_id", m.TypeInt), col("sentiment_score", m.TypeDecimal))))
	actions := placed("agent.sql", new(table("agent_actions", []string{"action_id"},
		col("action_id", m.TypeInt), nullable(new(col("model", m.TypeString))))))
	features := placed("features.sql", new(entityOf("customer_features",
		[]string{"layer", "feature"}, col("customer_id", m.TypeInt))))
	examples := placed("examples.sql", new(entityOf("training_examples",
		[]string{"layer", "training"}, col("example_id", m.TypeInt),
		col("is_synthetic", m.TypeBool))))
	marked := placed("marked.sql", new(entityOf("synthetic_orders",
		[]string{"synthetic", "possible"}, col("order_id", m.TypeInt),
		col("is_synthetic", m.TypeBool), col("generator_version", m.TypeString),
		col("source_dataset_id", m.TypeInt))))
	marked.Checks = []m.Check{{Columns: []string{"is_synthetic", "generator_version"}}}
	graphNodes := placed("nodes.sql", new(entityOf("nodes", []string{"graph", "node"},
		col("node_id", m.TypeInt))))
	graphNodes.PrimaryKey = []string{"node_id"}
	graphEdges := placed("edges.sql", new(table("edges", []string{"edge_id"},
		col("edge_id", m.TypeInt), col("from_node_id", m.TypeInt),
		nullable(new(col("to_node_id", m.TypeInt))),
		enumField("rel_type", []string{"KNOWS", "WORKS_AT"}))))
	graphEdges.Refs = []m.Ref{fk("from_node_id", "nodes", "set null")}
	return map[string]messageCase{
		"10.2-A payload":      {in: src(events), designs: []string{"10.2-A"}},
		"10.2-A remainder":    {in: src(orderSchema, orders), designs: []string{"10.2-A"}},
		"10.2-B union":        {in: src(orderSchema), designs: []string{"10.2-B"}},
		"10.3-A content":      {in: src(docs), designs: []string{"10.3-A"}},
		"10.4-A blended":      {in: src(reviews), designs: []string{"10.4-A"}},
		"10.6-A context":      {in: src(actions), designs: []string{"10.6-A"}},
		"10.7-A artifacts":    {in: src(features, actions), designs: []string{"10.7-A"}},
		"10.9-A markers":      {in: src(examples), designs: []string{"10.9-A"}},
		"10.9-A quality":      {in: src(marked), designs: []string{"10.9-A"}},
		"10.10-A endpoints":   {in: src(graphNodes, graphEdges), designs: []string{"10.10-A"}},
		"10.10-B closed":      {in: src(graphNodes, graphEdges), designs: []string{"10.10-B"}},
		"10.1-A unclassified": {in: src(audited), designs: []string{"10.1-A"}},
	}
}
