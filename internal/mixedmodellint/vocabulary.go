package mixedmodellint

// knownKey reports whether key belongs to the annotation vocabulary of
// rule_plan.md §2: the core keys and every rule-specific key. Any other key
// produces an mml/unknown-key note, so a typo such as "grian" cannot pass
// silently as an annotation nobody reads.
func knownKey(key string) bool {
	switch key {
	case "entity", "definition", "owner", "escalation", "synonyms", "grain", "layer",
		"lifecycle", "source", "role", "measure", "numerator", "denominator", "over", "null",
		"unit", "currency", "time", "tz", "inferred", "confidence", "threshold", "review_queue",
		"deviation", "waive", "value-object", "pk", "key", "natural_key", "fp", "card",
		"process", "freshness", "format", "format_reason", "change_policy", "out_of_scope",
		"ownership", "ai_systems_reason", "producers", "status", "standard", "agent_exposed",
		"agent_ops", "transitions", "translates", "mapping", "taxonomy", "contract", "term",
		"version", "renamed_from", "history_of", "history_in", "denormalized_for",
		"captured_at_transaction", "upstream", "master", "required", "enforced_in", "history",
		"reviewed_version", "resolution", "resolution_owner", "sources", "refers",
		"derived_from", "location", "denormalized_from", "split_reason", "pattern",
		"pattern_adr", "removal_candidate", "null_when", "consumers", "consequential",
		"validates", "max-items", "ref", "load", "rollups", "window", "schema",
		"recoverable_from", "recoverable_until", "counts", "active", "period", "count_source",
		"m2m", "approx_error", "reaggregate", "dimensions", "lateness", "late_policy",
		"breakdowns", "scale", "pooling", "key_metric", "reconciles_with", "tolerance",
		"encoding", "end", "interval", "lifespan", "audit", "precision", "synthetic", "core",
		"extension", "dedup", "derived", "aspects", "label", "observation_time", "actor",
		"monitored_by", "meta", "quality", "graph":
		return true
	default:
		return false
	}
}
