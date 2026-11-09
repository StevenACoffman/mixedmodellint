package mixedmodellint_test

import (
	"testing"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

func TestTrainingSetContract(t *testing.T) {
	t.Parallel()
	declared := []string{
		"layer", "training", "label", "churned", "observation_time", "snapshot_date",
	}
	training := func(kv []string, pk []string, fields ...m.Field) m.Entity {
		e := entityOf("churn_training", kv, append([]m.Field{
			col("customer_id", m.TypeInt), col("churned", m.TypeBool),
		}, fields...)...)
		e.PrimaryKey = pk
		return e
	}
	snapshot := col("snapshot_date", m.TypeDate)
	key := []string{"customer_id", "snapshot_date"}
	features := func(fields ...m.Field) m.Entity {
		return entityOf("customer_features", []string{"layer", "feature"}, append([]m.Field{
			col("customer_id", m.TypeInt), col("order_count", m.TypeInt),
		}, fields...)...)
	}
	withFeatures := func(e m.Entity) m.Entity {
		return referencing(&e, "customer_id", "customer_features")
	}
	cases := map[string]struct {
		entities []m.Entity
		want     []string
	}{
		"a complete contract": {entities: []m.Entity{training(declared, key, snapshot)}},
		"nothing declared": {
			entities: []m.Entity{training([]string{"layer", "training"}, key, snapshot)},
			want:     []string{"10.5-A churn_training"},
		},
		"the label is not a field": {
			entities: []m.Entity{training([]string{
				"layer", "training", "label", "is_churned", "observation_time", "snapshot_date",
			}, key, snapshot)},
			want: []string{"10.5-A churn_training"},
		},
		"a nullable observation time": {
			entities: []m.Entity{training(declared, key, nullableCol("snapshot_date",
				m.TypeDate))},
			want: []string{"10.5-A churn_training"},
		},
		"an observation time outside the key": {
			entities: []m.Entity{training(declared, []string{"customer_id"}, snapshot)},
			want:     []string{"10.5-A churn_training"},
		},
		"a feature table without an as-of time": {
			entities: []m.Entity{withFeatures(training(declared, key, snapshot)), features()},
			want:     []string{"10.5-A churn_training"},
		},
		"a feature table with an as-of time": {
			entities: []m.Entity{
				withFeatures(training(declared, key, snapshot)),
				features(col("as_of", m.TypeTimestamptz)),
			},
		},
		"named for training": {
			entities: []m.Entity{table("train_churn", nil, col("customer_id", m.TypeInt))},
			want:     []string{"10.5-A train_churn"},
		},
		"a label table in another layer": {
			entities: []m.Entity{entityOf("shipping_labels", []string{"layer", "oltp"},
				col("label_id", m.TypeInt))},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.want, findingsOf(src(tc.entities...), "10.5-A"))
		})
	}
}
