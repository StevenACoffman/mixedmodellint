package mixedmodellint_test

import (
	"testing"

	m "github.com/StevenACoffman/mixedmodellint/internal/mixedmodellint"
)

// documented returns a field of type lt with doc and annotations kv.
func documented(name string, lt m.LogicalType, doc string, kv ...string) m.Field {
	f := fieldWith(name, lt, kv...)
	f.Doc = doc
	return f
}

func TestMetadataClassification(t *testing.T) {
	t.Parallel()
	customers := func(kv []string, fields ...m.Field) m.Entity {
		e := entityOf(
			"customers",
			kv,
			append([]m.Field{col("customer_id", m.TypeInt)}, fields...)...)
		e.PrimaryKey = []string{"customer_id"}
		return e
	}
	ts := m.TypeTimestamptz
	cases := map[string]struct {
		entities []m.Entity
		want     []string
	}{
		"an audit column with no class": {
			entities: []m.Entity{customers(nil, col("updated_at", ts))},
			want:     []string{"10.1-A customers updated_at"},
		},
		"declared system and described": {
			entities: []m.Entity{customers(nil, documented("updated_at", ts,
				"When the pipeline last wrote the row (system time).", "time", "system"))},
		},
		"declared but undescribed": {
			entities: []m.Entity{customers(nil, fieldWith("updated_at", ts, "time", "business"))},
			want:     []string{"10.1-A customers updated_at"},
		},
		"a description that does not say which": {
			entities: []m.Entity{customers(nil, documented("updated_at", ts,
				"When the row changed.", "time", "business"))},
			want: []string{"10.1-A customers updated_at"},
		},
		"system by its leading underscore": {
			entities: []m.Entity{customers(nil, documented("_loaded_at", ts,
				"When the load wrote the row."))},
		},
		"system by name but undescribed": {
			entities: []m.Entity{customers(nil, col("_loaded_at", ts))},
			want:     []string{"10.1-A customers _loaded_at"},
		},
		"business by the entity's own prefix": {
			entities: []m.Entity{customers(nil, documented("customer_updated_at", ts,
				"When the customer last changed their details, a business time."))},
		},
		"an ambiguous name never classifies by name": {
			entities: []m.Entity{customers(nil, documented("version", m.TypeInt,
				"Bumped by the business on every edit."))},
			want: []string{"10.1-A customers version"},
		},
		"a non-temporal metadata column": {
			entities: []m.Entity{customers(nil, documented("batch_id", m.TypeInt,
				"The load batch that wrote the row."))},
		},
		"an event time is not metadata": {
			entities: []m.Entity{customers(nil, col("signed_up_at", ts))},
		},
		"another form classifies it": {
			entities: []m.Entity{
				customers(nil, col("updated_at", ts)),
				{
					ID: "go:example.com/model.Customer", Kind: m.KindStruct, Source: m.SourceGo,
					Name: "Customer", Annotations: m.Annotations{},
					Fields: []m.Field{documented("UpdatedAt", ts,
						"When the pipeline last wrote the row (system time).", "time", "system")},
				},
			},
		},
		"a third-party schema": {
			entities: []m.Entity{
				customers([]string{"source", "third_party"}, col("updated_at", ts)),
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.want, findingsOf(src(tc.entities...), "10.1-A"))
		})
	}
}

func TestRawBlobs(t *testing.T) {
	t.Parallel()
	json := m.TypeJSON
	events := func(kv []string, fields ...m.Field) m.Entity {
		e := entityOf("events", kv, append([]m.Field{col("event_id", m.TypeInt)}, fields...)...)
		e.PrimaryKey = []string{"event_id"}
		return e
	}
	orderSchema := m.Entity{
		ID: "json:schemas/order.json#", Kind: m.KindSchema, Source: m.SourceJSONSchema,
		Name: "Order", Annotations: ann("extension", "attributes"),
		Fields: []m.Field{col("order_id", m.TypeInt), col("attributes", m.TypeObject)},
	}
	cases := map[string]struct {
		entities []m.Entity
		want     []string
	}{
		"only a payload besides keys and times": {
			entities: []m.Entity{events(nil, col("received_at", m.TypeTimestamptz),
				col("payload", json))},
			want: []string{"10.2-A events payload"},
		},
		"a blob-named column and too few typed ones": {
			entities: []m.Entity{events(nil, col("kind", m.TypeString), col("data", json))},
			want:     []string{"10.2-A events data"},
		},
		"a blob-named column beside enough typed ones": {
			entities: []m.Entity{events(nil, col("kind", m.TypeString),
				col("amount", m.TypeDecimal), col("occurred_at", m.TypeTimestamptz),
				col("data", json))},
		},
		"a remainder column beside typed ones": {
			entities: []m.Entity{events(nil, col("kind", m.TypeString), col("attributes", json))},
		},
		"a staging table lands payloads": {
			entities: []m.Entity{events([]string{"layer", "staging"}, col("payload", json))},
		},
		"a schema's extension point with nowhere to go": {
			entities: []m.Entity{
				orderSchema,
				table("orders", []string{"order_id"}, col("order_id", m.TypeInt)),
			},
			want: []string{"10.2-A orders"},
		},
		"a schema's extension point with a column": {
			entities: []m.Entity{
				orderSchema,
				table("orders", []string{"order_id"}, col("order_id", m.TypeInt),
					col("status", m.TypeString), col("attributes", json)),
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.want, findingsOf(src(tc.entities...), "10.2-A"))
		})
	}
}

func TestSchemaShapes(t *testing.T) {
	t.Parallel()
	prop := func(raw string, required bool, kv ...string) m.Field {
		f := fieldWith("category", m.TypeUnknown, kv...)
		f.RawType, f.Nullable = raw, !required
		return f
	}
	cases := map[string]struct {
		field m.Field
		want  []string
	}{
		"a required union of kinds": {
			field: prop("array|object|string", true),
			want:  []string{"10.2-B Product category"},
		},
		"an optional union": {field: prop("array|object|string", false)},
		"an optional core union": {
			field: prop("array|string", false, "core", "true"),
			want:  []string{"10.2-B Product category"},
		},
		"a required property with no type": {
			field: prop("", true),
			want:  []string{"10.2-B Product category"},
		},
		"a required oneOf": {field: prop("oneOf", true)},
		"a required string": {
			field: m.Field{Name: "category", Type: m.TypeString, RawType: "string"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e := m.Entity{
				ID: "json:schemas/product.json#", Kind: m.KindSchema, Source: m.SourceJSONSchema,
				Name: "Product", Annotations: m.Annotations{}, Fields: []m.Field{tc.field},
			}
			equals(t, tc.want, findingsOf(src(e), "10.2-B"))
		})
	}
}

func TestUnstructuredContent(t *testing.T) {
	t.Parallel()
	docs := func(fields ...m.Field) m.Entity {
		return table("documents", []string{"document_id"},
			append([]m.Field{col("document_id", m.TypeUUID)}, fields...)...)
	}
	stored := []m.Field{
		col("file_uri", m.TypeString), col("uploaded_by", m.TypeInt),
		col("uploaded_at", m.TypeTimestamptz), col("content_type", m.TypeString),
		col("content_sha256", m.TypeString),
	}
	cases := map[string]struct {
		entity m.Entity
		want   []string
	}{
		"file bytes in the row": {
			entity: docs(col("body", m.TypeBytes)),
			want:   []string{"10.3-A documents body"},
		},
		"a hash is bytes, not a file": {entity: docs(col("content_hash", m.TypeBytes))},
		"a signing key":               {entity: docs(col("signing_key", m.TypeBytes))},
		"an image kept as text": {
			entity: docs(col("photo_data", m.TypeString)),
			want:   []string{"10.3-A documents photo_data"},
		},
		"a reference with its metadata": {entity: docs(stored...)},
		"a reference missing who and what": {
			entity: docs(stored[0], stored[2], stored[4]),
			want:   []string{"10.3-A documents file_uri"},
		},
		"a web address is no stored object": {entity: docs(col("website_url", m.TypeString))},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.want, findingsOf(src(tc.entity), "10.3-A"))
		})
	}
}

func TestBlendedSentiment(t *testing.T) {
	t.Parallel()
	reviews := func(fields ...m.Field) m.Entity {
		return table("reviews", []string{"review_id"},
			append([]m.Field{col("review_id", m.TypeInt)}, fields...)...)
	}
	aspects := referencing(new(table("review_aspect_sentiments", []string{"review_id", "aspect"},
		col("review_id", m.TypeInt), col("aspect", m.TypeString), col("score", m.TypeDecimal))),
		"review_id", "reviews")
	cases := map[string]struct {
		entities []m.Entity
		want     []string
	}{
		"one blended score": {
			entities: []m.Entity{reviews(col("sentiment_score", m.TypeDecimal))},
			want:     []string{"10.4-A reviews sentiment_score"},
		},
		"an overall polarity": {
			entities: []m.Entity{reviews(col("overall_polarity", m.TypeFloat))},
			want:     []string{"10.4-A reviews overall_polarity"},
		},
		"a blend beside per-aspect scores": {
			entities: []m.Entity{reviews(col("sentiment_score", m.TypeDecimal),
				col("sentiment_product", m.TypeDecimal), col("sentiment_shipping", m.TypeDecimal))},
		},
		"a blend beside an aspect table": {
			entities: []m.Entity{reviews(col("sentiment_score", m.TypeDecimal)), aspects},
		},
		"a sentiment label is not a score": {
			entities: []m.Entity{reviews(col("sentiment", m.TypeString))},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			equals(t, tc.want, findingsOf(src(tc.entities...), "10.4-A"))
		})
	}
}
