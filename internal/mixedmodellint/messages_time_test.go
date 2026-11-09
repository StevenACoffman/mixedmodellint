package mixedmodellint_test

import (
	"testing"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// timeMessageCases covers the designs about versions, calendars and training
// sets (§9.5, §9.6, §9.7, §10.5): one input per message variant.
func timeMessageCases(t *testing.T) map[string]messageCase {
	t.Helper()
	from, to := col("valid_from", m.TypeDate), nullableCol("valid_to", m.TypeDate)
	sentinel := col("valid_to", m.TypeDate)
	sentinel.Default = "9999-12-31"
	unordered := placed("prices.sql", new(prices("product_prices", false,
		nullableCol("valid_from", m.TypeDate), to)))
	nulls := placed("addresses.sql", new(prices("customer_addresses", true, from, to)))
	plans := placed("plans.sql", new(prices("plans", true, from, to)))
	tiers := placed("tiers.sql", new(prices("price_tiers", true, from, sentinel)))
	tickets := placed("tickets.sql", new(entityOf("tickets", []string{"audit", "bitemporal"},
		col("ticket_id", m.TypeInt), col("valid_from", m.TypeTimestamptz),
		nullableCol("valid_to", m.TypeTimestamptz), col("txn_from", m.TypeTimestamptz))))
	tickets.PrimaryKey = []string{"ticket_id"}
	dimDate := placed("dim_date.sql", new(table("dim_date", []string{"calendar_date"},
		col("calendar_date", m.TypeDate), col("fiscal_year", m.TypeInt))))
	fiscal := placed("fiscal.sql", new(table("fiscal_calendar", []string{"day"},
		col("day", m.TypeDate), col("fiscal_year", m.TypeInt), col("fiscal_quarter", m.TypeInt),
		col("fiscal_month", m.TypeInt), col("fiscal_week", m.TypeInt),
		col("is_fiscal_year_end", m.TypeBool))))
	churn := placed("churn.sql", new(entityOf("churn_training", []string{
		"layer", "training",
		"label", "churned", "observation_time", "snapshot_date",
	},
		col("customer_id", m.TypeInt), col("churned", m.TypeBool),
		nullableCol("snapshot_date", m.TypeDate))))
	churn.PrimaryKey = []string{"customer_id"}
	churn = referencing(&churn, "customer_id", "customer_features")
	feats := placed("features.sql", new(entityOf("customer_features",
		[]string{"layer", "feature"}, col("customer_id", m.TypeInt))))
	bare := placed("train.sql", new(table("train_churn", nil, col("customer_id", m.TypeInt))))
	monthly := placed("sales.sql", new(table("monthly_sales", []string{"sale_id"},
		col("sale_id", m.TypeInt), col("fiscal_year", m.TypeInt),
		col("fiscal_quarter", m.TypeInt))))
	return map[string]messageCase{
		"9.5-A gaps":        {in: src(unordered), designs: []string{"9.5-A"}},
		"9.5-A open ends":   {in: src(nulls, plans, tiers), designs: []string{"9.5-A"}},
		"9.6-A gaps":        {in: src(tickets), designs: []string{"9.6-A"}},
		"9.7-A calendars":   {in: src(dimDate, fiscal, monthly), designs: []string{"9.7-A"}},
		"10.5-A gaps":       {in: src(churn, feats), designs: []string{"10.5-A"}},
		"10.5-A undeclared": {in: src(bare), designs: []string{"10.5-A"}},
		"9.7-A no calendar": {in: src(monthly), designs: []string{"9.7-A"}},
	}
}
