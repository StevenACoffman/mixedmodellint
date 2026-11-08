package mixedmodellint_test

import (
	"slices"
	"testing"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// shapeMessageCases covers the designs about entity shape and keys (§1.9,
// §3.3, §3.5, §4, §5, §6, §7.6, §8.8, §8.9): one input per message variant.
func shapeMessageCases(t *testing.T) map[string]messageCase {
	t.Helper()
	shortcut := placed("products.sql", new(table("products", []string{"product_id"},
		col("product_id", m.TypeInt),
		documented("attributes", m.TypeJSON, "Attributes as JSON for now."))))
	halfADR := src(shortcut)
	halfADR.Artifacts.ADRs = []m.Document{{
		Path: "docs/adr/0007-catalog-json.md", Text: "## Rationale\n\nLaunch deadline.\n",
	}}
	recorded := shortcut
	recorded.Fields = append([]m.Field(nil), shortcut.Fields...)
	recorded.Fields[1].Annotations = ann("deviation", "0007-catalog-json")
	halfADR.Sources[0].Entities = []m.Entity{recorded}
	stg := placed("stg.sql", new(entityOf("stg_orders", []string{"layer", "staging"},
		col("order_id", m.TypeInt))))
	obt := placed("report.sql", new(wide("order_report", 50, "layer", "obt",
		"upstream", "stg_orders, carts")))
	vendor := placed("db/001_vendor.sql", new(entityOf("vendor_orders",
		[]string{"source", "third_party"}, col("id", m.TypeInt))))
	vendor.Changes = []m.Change{{
		Statement: "ALTER TABLE", Pos: m.Position{File: "db/007_extend.sql", Line: 3},
	}}
	people := placed("people.sql", new(table("people", []string{"first_name", "last_name"},
		col("first_name", m.TypeString), col("last_name", m.TypeString))))
	visits := placed("visits.sql", new(table(
		"visits",
		[]string{"visit_id"},
		col(
			"visit_id",
			m.TypeInt,
		),
		col("first_name", m.TypeString),
		col("last_name", m.TypeString),
	)))
	visits.Refs = []m.Ref{{Fields: []string{"first_name", "last_name"}, Target: "people"}}
	merged := placed("customers.sql", new(table("customers", []string{"email"},
		col("email", m.TypeString), col("crm_id", m.TypeString), col("erp_id", m.TypeString))))
	legacy := placed("accounts.sql", new(table("accounts", []string{"account_id"},
		col("account_id", m.TypeInt), col("legacy_crm_id", m.TypeString))))
	customers := placed("crm.sql", new(table("customers", []string{"customer_id"},
		col("customer_id", m.TypeInt))))
	tickets := placed("tickets.sql", new(table("tickets", nil, col("customer_name", m.TypeString),
		col("reviewer_email", m.TypeString))))
	images := placed("images.sql", new(table("images", []string{"sha256"},
		col("sha256", m.TypeString), col("storage_path", m.TypeString))))
	god := placed("customers.sql", new(godEntity(27, nil,
		nullableCol("billing_street", m.TypeString), nullableCol("billing_city", m.TypeString),
		nullableCol("billing_zip", m.TypeString))))
	loyal := placed("loyal.sql", new(table("customers", []string{"customer_id"},
		col("customer_id", m.TypeInt))))
	orders := placed("orders.sql", new(referencing(new(table("orders", []string{"order_id"},
		col("order_id", m.TypeInt), col("customer_id", m.TypeInt),
		col("customer_loyalty_tier", m.TypeString))), "customer_id", "customers")))
	unlinked := placed("orders.sql", new(table("orders", []string{"order_id"},
		col("order_id", m.TypeInt), col("customer_loyalty_tier", m.TypeString))))
	products := placed("products.sql", new(table("products", []string{"product_id"},
		col("product_id", m.TypeInt))))
	xref := placed("xref.sql", new(referencing(new(referencing(new(entityOf(
		"customer_product_xref", []string{"definition", "Rows the old loader wrote."},
		col("customer_id", m.TypeInt), col("product_id", m.TypeInt))),
		"customer_id", "customers")), "product_id", "products")))
	ticket := func(name string) m.Entity {
		return placed(name+".sql", new(table(name, []string{"ticket_id"},
			col("ticket_id", m.TypeInt), col("subject", m.TypeString), col("body", m.TypeString),
			col("priority", m.TypeString), col("opened_at", m.TypeTimestamptz),
			col("requester_email", m.TypeString))))
	}
	contacts := []m.Entity{
		placed("customers.sql", new(contactTable("customers", ""))),
		placed("employees.sql", new(contactTable("employees", ""))),
		placed("vendors.sql", new(contactTable("vendors", "vendor_"))),
	}
	stale := slices.Clone(contacts)
	for i := range stale {
		stale[i].Annotations = ann("pattern", `party_role=rejected reason="self-joins" members=2`)
	}
	styled := placed("orders.sql", new(snakeTable("orders", col("shippedAt", m.TypeTimestamptz),
		col("PlacedAt", m.TypeTimestamptz))))
	plurals := []m.Entity{
		placed("a.sql", new(snakeTable("orders"))), placed("b.sql", new(snakeTable("customers"))),
		placed("c.sql", new(snakeTable("invoices"))), placed("d.sql", new(snakeTable("payments"))),
		placed("e.sql", new(snakeTable("shipment"))),
	}
	cryptic := placed("lines.sql", new(table("order_lines", nil, col("cust_qty", m.TypeInt),
		col("c_3", m.TypeInt))))
	codes := placed("orders.sql", new(table("orders", nil, col("status", m.TypeString),
		enumField("hold_reason", []string{"fraud", "FRAUD", "defect", "defective"}))))
	sqlOrders := placed("orders.sql", new(table("orders", nil,
		enumField("status", []string{"open", "closed"}))))
	jsonOrder := placed("order.schema.json", new(table("order", nil,
		enumField("status", []string{"open", "closed", "held"}))))
	jsonOrder.ID, jsonOrder.Source = "json:order", m.SourceJSONSchema
	spouses := placed("people.sql", new(referencing(new(table("people", []string{"person_id"},
		col("person_id", m.TypeInt), col("spouse_id", m.TypeInt),
		col("spouse_since", m.TypeDate))), "spouse_id", "people")))
	students := placed("students.sql", new(table("students", []string{"student_id"},
		col("student_id", m.TypeInt))))
	courses := placed("courses.sql", new(table("courses", []string{"course_id"},
		col("course_id", m.TypeInt))))
	enrollments := placed("student_courses.sql", new(referencing(new(referencing(new(table(
		"student_courses", []string{"student_id", "course_id"}, col("student_id", m.TypeInt),
		col("course_id", m.TypeInt), col("grade", m.TypeString),
		col("enrolled_on", m.TypeDate))), "student_id", "students")), "course_id", "courses")))
	transcripts := placed("transcripts.sql", new(table("transcripts", nil,
		col("student_id", m.TypeInt), col("course_id", m.TypeInt))))
	transcripts.Refs = []m.Ref{{
		Fields: []string{"student_id", "course_id"},
		Target: "student_courses",
	}}
	lockedPeople := referencing(new(table("people", []string{"person_id"},
		col("person_id", m.TypeInt), col("spouse_id", m.TypeInt))), "spouse_id", "people")
	grown := src(spouses)
	grown.Lock = lockOf(t, lockedPeople)
	history := placed("customer.schema.json", new(table("customer", nil,
		col("order_history", m.TypeArray))))
	history.Source = m.SourceJSONSchema
	headers := placed("orders.sql", new(table("orders", []string{"order_id"},
		col("order_id", m.TypeInt))))
	priced := placed("products.sql", new(table("products", []string{"product_id"},
		col("product_id", m.TypeInt), m.Field{
			Name: "unit_price", Type: m.TypeDecimal,
			RawType: "numeric(12,2)",
		}, col("valid_from", m.TypeDate))))
	lineItems := placed("order_lines.sql", new(referencing(new(referencing(new(table(
		"order_lines", []string{"order_id", "line_no"}, col("order_id", m.TypeInt),
		col("line_no", m.TypeInt), col("product_id", m.TypeInt))), "order_id", "orders")),
		"product_id", "products")))
	inferred := placed("matches.sql", new(table("customer_matches", nil,
		col("left_id", m.TypeInt), col("right_id", m.TypeInt), col("score", m.TypeFloat))))
	exposed := placed("readings.sql", new(entityOf("readings", []string{
		"agent_exposed", "true",
		"grain", "one row per sensor per hour", "rollups", "hour, weekly_readings",
	},
		col("sensor_id", m.TypeInt), col("reading_hour", m.TypeTimestamptz))))
	nominal := placed("customers.sql", new(referencing(new(table("customers", nil,
		fieldWith("store_id", m.TypeInt, "measure", "additive"),
		fieldWith("zip_code", m.TypeInt, "measure", "additive"),
		m.Field{Name: "embedding", RawType: "vector(1536)"})), "store_id", "stores")))
	revenue := placed("daily_revenue.sql", new(entityOf("daily_revenue",
		[]string{"key_metric", "true"}, fieldWith("revenue_usd", m.TypeDecimal,
			"measure", "additive", "currency", "USD",
			"reconciles_with", "order_lines.revenue_eur"))))
	eur := placed("order_lines.sql", new(table("order_lines", nil, fieldWith("revenue_eur",
		m.TypeDecimal, "measure", "additive", "currency", "EUR"))))
	return map[string]messageCase{
		"7.6-A rollups": {in: src(exposed), designs: []string{"7.6-A"}},
		"8.8-A nominal": {in: src(nominal), designs: []string{"8.8-A"}},
		"8.9-B units":   {in: src(revenue, eur), designs: []string{"8.9-B"}},
		"8.9-B none": {
			in: src(
				placed(
					"r.sql",
					new(
						entityOf(
							"daily_revenue",
							[]string{"key_metric", "true"},
							fieldWith("revenue_usd", m.TypeDecimal, "measure", "additive"),
						),
					),
				),
			),
			designs: []string{"8.9-B"},
		},
		"6.4-A self pair": {in: src(spouses), designs: []string{"6.4-A"}},
		"6.4-B fat junction": {
			in:      src(students, courses, enrollments, transcripts),
			designs: []string{"6.4-B"},
		},
		"6.4-C since the lock": {in: grown, designs: []string{"6.4-C"}},
		"6.5-B unbounded":      {in: src(history), designs: []string{"6.5-B"}},
		"6.6-A no snapshot": {
			in:      src(headers, priced, lineItems),
			designs: []string{"6.6-A"},
		},
		"6.7-A incomplete":         {in: src(inferred), designs: []string{"6.7-A"}},
		"5.2-A style and spelling": {in: src(styled), designs: []string{"5.2-A"}},
		"5.2-A number":             {in: src(plurals...), designs: []string{"5.2-A"}},
		"5.2-B cryptic":            {in: src(cryptic), designs: []string{"5.2-B"}},
		"5.4-A and B codes":        {in: src(codes), designs: []string{"5.4-A", "5.4-B"}},
		"5.4-B forms disagree":     {in: src(sqlOrders, jsonOrder), designs: []string{"5.4-B"}},
		"4.11-A cluster":           {in: src(contacts...), designs: []string{"4.11-A"}},
		"4.11-B no record": {
			in:      src(contacts[0], contacts[1], contacts[2]),
			designs: []string{"4.11-B"},
		},
		"4.11-B grown group":  {in: src(stale...), designs: []string{"4.11-B"}},
		"4.8-A sparse groups": {in: src(god), designs: []string{"4.8-A"}},
		"4.8-B referenced":    {in: src(loyal, orders), designs: []string{"4.8-B"}},
		"4.8-B name only":     {in: src(loyal, unlinked), designs: []string{"4.8-B"}},
		"4.9-A unnamed sides": {in: src(loyal, products, xref), designs: []string{"4.9-A"}},
		"4.10-A channels": {
			in:      src(ticket("chat_tickets"), ticket("email_tickets")),
			designs: []string{"4.10-A"},
		},
		"4.10-A structure": {
			in:      src(ticket("support_requests"), ticket("complaints")),
			designs: []string{"4.10-A"},
		},
		"4.4-A composite": {in: src(people, visits), designs: []string{"4.4-A"}},
		"4.4-A merged":    {in: src(merged), designs: []string{"4.4-A"}},
		"4.4-B unindexed": {in: src(legacy), designs: []string{"4.4-B"}},
		"4.5-A display":   {in: src(customers, tickets), designs: []string{"4.5-A"}},
		"4.7-A hash key":  {in: src(images), designs: []string{"4.7-A"}},
		"3.3-B no contract": {
			in:      src(placed("wide.sql", new(wide("order_report", 50)))),
			designs: []string{"3.3-B"},
		},
		"3.3-B bad upstream": {in: src(obt, stg), designs: []string{"3.3-B"}},
		"3.5-A alter":        {in: src(vendor), designs: []string{"3.5-A"}},
		"1.9-B no record":    {in: src(shortcut), designs: []string{"1.9-B"}},
		"1.9-B no cost":      {in: halfADR, designs: []string{"1.9-B"}},
	}
}
