package mixedmodellint

import "regexp"

// contentLexicon holds the names by which the §10 rules recognize what kind
// of content a field holds: metadata about a row rather than the business,
// and which of the two a metadata name already says; whole records kept as
// JSON; files kept in rows or referenced in storage; sentiment scores;
// quality records; graph edges and their relationship types.
type contentLexicon struct {
	metadataName *regexp.Regexp // 10.1-A: attributes about the row, not the business
	systemName   *regexp.Regexp // 10.1-A: metadata names that can only be system
	classWord    *regexp.Regexp // 10.1-A: a description saying business or system
	blobName     *regexp.Regexp // 10.2-A: JSON columns that hold a whole record

	secretBytes *regexp.Regexp // 10.3-A: bytes that are a hash or key, not a file
	mediaText   *regexp.Regexp // 10.3-A: text columns that hold a file
	objectRef   *regexp.Regexp // 10.3-A: references to an object in storage
	objectMeta  []objectMetaKind

	blendedSentiment *regexp.Regexp // 10.4-A: one score for every aspect
	aspectSentiment  *regexp.Regexp // 10.4-A: a score for one aspect

	qualityField *regexp.Regexp // 10.9-A: a record of how good generated data is

	relTypeField *regexp.Regexp // 10.10: the field naming an edge's relationship
	edgeName     *regexp.Regexp // 10.10: tables named as a graph's edges
}

// objectMetaKind is one thing §10.3-A asks to be known about a stored
// object: who stored it, when, what it is, which version.
type objectMetaKind struct {
	kind    string
	example string
	name    *regexp.Regexp
}

func newContentLexicon() contentLexicon {
	return contentLexicon{
		metadataName: regexp.MustCompile(`(?i)(^_|(^|_)(created|updated|modified|changed|` +
			`deleted|loaded|synced|ingested|processed|extracted)_(at|on|by|ts|time)$|` +
			`(^|_)(last_updated|etl_\w+|batch_id|run_id|source_system|record_source|row_hash|` +
			`etag|version)$)`),
		systemName: regexp.MustCompile(`(?i)(^_|(^|_)(loaded|etl|ingested|synced|dbt|batch|` +
			`run)(_|$))`),
		classWord: regexp.MustCompile(`(?i)(\b(business|system|pipeline|load)\b|^@(time|meta):)`),
		blobName:  regexp.MustCompile(`(?i)(^|_)(payload|data|body|raw|blob|doc|json)(_|$)`),
		secretBytes: regexp.MustCompile(`(?i)(^|_)(hash|sha\d*|key|nonce|salt|signature|token|` +
			`iv|digest|checksum|hmac)(_|$)`),
		mediaText: regexp.MustCompile(`(?i)(^|_)(image|photo|video|audio|pdf|file|attachment)` +
			`(_(data|content|blob|base64))?$`),
		objectRef: regexp.MustCompile(`(?i)(^|_)(object_key|(file|image|photo|video|audio|pdf|` +
			`attachment|document|blob|object|content|media|storage)_(uri|url|key|path))$`),
		objectMeta: objectMetaKinds(),
		blendedSentiment: regexp.MustCompile(
			`(?i)^(overall_|total_|avg_|blended_)?(sentiment|polarity|tone)(_score)?$`),
		aspectSentiment: regexp.MustCompile(`(?i)^sentiment_\w+$`),
		qualityField:    regexp.MustCompile(`(?i)(quality|fidelity|validation)_(score|status)$`),
		relTypeField: regexp.MustCompile(
			`(?i)^(rel|relationship|edge|predicate|link)_?(type|kind|name)(_id)?$`),
		edgeName: regexp.MustCompile(`(?i)(^|_)(edges?|relationships?|triples?|links?)$`),
	}
}

func objectMetaKinds() []objectMetaKind {
	kind := func(kind, example, names string) objectMetaKind {
		return objectMetaKind{
			kind: kind, example: example, name: regexp.MustCompile(`(?i)^(` + names + `)$`),
		}
	}
	return []objectMetaKind{
		kind("who stored it", "uploaded_by", "uploaded_by|uploader_id|owner_id|created_by"),
		kind("when it was stored", "uploaded_at", "uploaded_at|created_at"),
		kind("what it is", "content_type", "content_type|mime_type|mime|media_type"),
		kind("which version it is", "content_sha256",
			"version|content_sha256|sha256|checksum|etag|content_hash"),
	}
}
