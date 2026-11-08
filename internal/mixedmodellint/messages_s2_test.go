package mixedmodellint_test

import (
	"testing"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// s2MessageCases covers the §2 designs: one input per message variant.
func s2MessageCases(t *testing.T) map[string]messageCase {
	t.Helper()
	customers := placed("crm.sql", new(inSchema("crm", "customers", []string{"customer_id"},
		col("customer_id", m.TypeInt))))
	tickets := placed("support.sql", new(referencing(
		new(inSchema("support", "tickets", []string{"ticket_id"},
			col("ticket_id", m.TypeInt), col("customer_id", m.TypeInt))),
		"customer_id", "crm.customers")))
	products := placed("products.sql", new(table(
		"products",
		[]string{"product_id"},
		col(
			"product_id",
			m.TypeInt,
		),
		col("category", m.TypeString),
		col("sales_region", m.TypeString),
	)))
	regions := placed("regions.sql", new(entityOf("regions", []string{"layer", "reference"},
		col("region_id", m.TypeInt))))
	sparse := placed("orders.sql", new(entityOf("orders", []string{"status", "done"},
		col("order_id", m.TypeInt), col("total", m.TypeDecimal))))
	shipments := placed("shipments.sql", new(table("shipments", nil,
		enumField("destination", []string{"US", "UK", "FR", "DE"}),
		enumField("charged_in", []string{"USD", "EUR", "GBP"}, "standard", "ISO 4217"))))
	patients := placed("patients.sql", new(table("patients", nil,
		col("diagnosis_code", m.TypeString),
		enumField("country", []string{"US", "GB", "Scotland", "Wales"}))))
	agentTickets := placed("tickets.sql", new(entityOf("tickets",
		[]string{"agent_ops", "read"}, col("ticket_id", m.TypeInt), col("status", m.TypeString))))
	agentTickets.Grants = []m.Grant{{Role: "support_agent", Privileges: []string{"UPDATE"}}}
	return map[string]messageCase{
		"2.2-A crossing":     {in: src(customers, tickets), designs: []string{"2.2-A"}},
		"2.5-A gaps":         {in: src(sparse), designs: []string{"2.5-A"}},
		"2.7-A unstated":     {in: src(patients), designs: []string{"2.7-A"}},
		"2.7-B variants":     {in: src(shipments), designs: []string{"2.7-B"}},
		"2.8-A contradicted": {in: src(agentTickets), designs: []string{"2.8-A"}},
		"2.3-A free text":    {in: src(products, regions), designs: []string{"2.3-A"}},
	}
}
