# mixedmodellint

`mixedmodellint` checks a code base's data model against the rules in
[`revised_data_model_rules.md`](revised_data_model_rules.md) in CI, without an
LLM. It reads SQL migrations, persisted Go structs and JSON Schemas into one
intermediate representation, links the same entity across them, and reports
each violation with the measured value and a suggested fix.
[`rule_plan.md`](rule_plan.md) describes every design; each finding carries
the design ID it has there, such as `7.1-B`.

```console
$ mixedmodellint check --dir .
db/schema.sql:4:1: error: [7.1-B] grain "one row per order" implies key (order_id), but order_lines has primary key (order_id, line_no)
    suggestion: Correct the grain statement to match the key (the key implies "one row per order line"); or, if "one row per order" is what a row means, add UNIQUE (order_id).
1 error(s), 0 warning(s), 0 note(s) in 2 entities and 0 queries
```

## Usage

```sh
mixedmodellint check    [--dir DIR] [--format text|json|sarif] [--output FILE]
                        [--baseline FILE] [--write-baseline FILE] [--base REF]
mixedmodellint lock     [--dir DIR] [--output FILE]   # write mml.lock.json
mixedmodellint annotate [--dir DIR]                   # print missing annotations
mixedmodellint explain  DESIGN|RULE                   # e.g. explain 7.1-B
```

- The exit status is 1 when any finding is an error. A `[MUST]` rule reports
  errors, a `[SHOULD]` rule warnings, and a `[CONSIDER]` rule notes. A
  heuristic design reports one level lower.
- `--format sarif` writes SARIF 2.1.0 for GitHub code scanning.
- To adopt the linter on an existing code base, run `--write-baseline
  mml-baseline.json` once. Then pass `--baseline mml-baseline.json`, so that
  only new findings fail.
- `lock` records every entity's structure in `mml.lock.json` (mechanism M3
  of rule_plan.md). Commit it with each model change: `check` reports a
  model that differs from it as `mml/lock-stale`, and compares the change
  with it to find new tables (1.1-B), renamed keys (4.3-A) and removed data
  (7.5-B) without git history.
- `--base REF` compares with the base revision instead, such as
  `--base origin/main`. REF is a branch, tag or commit ID that
  `git check-ref-format --allow-onelevel` accepts; for a revision
  expression, pass its commit ID: `--base "$(git rev-parse HEAD~1)"`. The
  linter exports `--dir` at REF with `git archive`, so uncommitted changes
  and ignored files are not part of the base.
- `annotate` prints, as an `mml.yaml` fragment, every annotation the
  declaration designs ask for and the model lacks. Values the structure
  implies are filled in (the grain from the key, with its `fp=`; a measure
  class from the field's name); the rest are `TODO`, which no design
  accepts. It never writes files: review the fragment and merge it.
- `explain` prints a design of rule_plan.md as the plan states it, and
  whether this build implements it.

### What is read

| Source      | Files                                                     | Entities                                                                                                                                                                                                                                                                                                                                                                                                  |
| ----------- | --------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| SQL         | `*.sql`, replayed in path order                           | `CREATE TABLE` and `CREATE VIEW` after every `ALTER` and `DROP`, with their grants, indexes, and the statements that changed a table after its `CREATE`. Other statements form the query corpus.                                                                                                                                                                                                          |
| Go          | Each Go module (a `go.mod`), loaded with type information | Structs with a storage tag (`db`, `gorm`, `bun`, `sql`, `bson`, `parquet`, `avro`), an `mml` tag, or an `//mml:` directive, including fields embedded from other packages. A struct with only `json` tags is a payload, not stored data. SQL passed to `database/sql`, pgx, sqlx or gorm, and to any interface method named like a `database/sql` method (such as sqlc's `DBTX`), joins the query corpus. |
| JSON Schema | `*.schema.json`, or JSON with `"$schema"`                 | The root object and each object in `$defs` or `definitions`                                                                                                                                                                                                                                                                                                                                               |
| Sidecar     | `mml.yaml` at the root                                    | Annotations for any entity, and the `owners:` ownership records (§1.13)                                                                                                                                                                                                                                                                                                                                   |
| Lock        | `mml.lock.json` at the root                               | The previous model, which designs that look at a change compare with                                                                                                                                                                                                                                                                                                                                      |

SQL in Go is found by building SSA for the module, so a query assembled from
constants (`"SELECT ... FROM " + table`) or chosen in an `if` is read in each
of its forms. SQL built at run time, such as with `fmt.Sprintf`, is not.
Findings in that SQL point at the Go call. Loading Go needs the `go` command
and the module's dependencies.

SSA also traces the values bound to a query's `$n` parameters: a field read
from a persisted struct (directly, or through an argument struct such as
sqlc's `CreateXParams`) is a copy of that field, and `Truncate`, `Round`,
`Format` and `time.Unix(t.Unix(), 0)` truncate a time. Findings about those
values point at the Go line that copies or truncates them.

sqlx's `:name` parameters are rewritten to `$n` and bound to the argument
struct's fields; gorm's `Create` and `Save` are writes of every stored field
of the model; and a helper that only forwards SQL and its arguments to
`ExecContext` is followed to each of its callers.

[sqlc](https://sqlc.dev) keeps each query in a `.sql` file and copies it into
generated Go. A statement in a `.sql` file and a Go constant with the same
`-- name:` header are read as one query, positioned in the `.sql` file, so
findings about it are not reported twice.

Some designs also read artifacts other than the model:

| Artifact          | Files                                                    | Read by              |
| ----------------- | -------------------------------------------------------- | -------------------- |
| Decision records  | `docs/adr/*.md`                                          | 1.1-B, 1.9-B, 4.11-B |
| Logical model     | `model/logical/*.yaml` (`entity`, `attributes`, `keys`)  | 1.1-B                |
| Ownership records | `model/ownership/<owner>.yaml`, or `owners:` in mml.yaml | 1.13-A, 4.2-A        |
| CODEOWNERS        | `CODEOWNERS`, `.github/CODEOWNERS`, `docs/CODEOWNERS`    | 1.2-A, 4.2-A         |

The walk skips hidden directories other than `.github`, and directories
whose names start with `_`. It also skips `vendor`, `node_modules`,
`testdata`, `bin`, `dist` and `target`.

### Annotations

Some rules need a declaration before they can check anything, such as a
grain or a load pattern. Each source kind has its own syntax for the same
keys, listed in rule_plan.md §2:

```sql
-- mml:grain one row per order
CREATE TABLE orders (...);
COMMENT ON COLUMN orders.total IS '@unit: USD';
```

```go
//mml:grain one row per order
type Order struct {
    Total int64 `db:"total" mml:"unit=USD;measure=additive"`
}
```

```json
{ "x-mml-grain": "one row per order", "properties": { "total": { "x-mml-unit": "USD" } } }
```

JSON Schema's own `maxItems` counts as `x-mml-max-items`, so a bounded
array need not say so twice.

A waiver needs a reason. A waiver without one waives nothing and is itself
reported:

```sql
-- mml:waive 5.1 reason="vendor schema we do not control"
```

## Implemented designs

This release implements the foundation of rule_plan.md and the designs
below; `mixedmodellint explain <design>` prints any design's full text.
The remaining designs are listed under [Not yet implemented](#not-yet-implemented).

| Design  | Checks                                                                                                  |
| ------- | ------------------------------------------------------------------------------------------------------- |
| 1.1-B   | A table, struct or schema new since the lock or base without a logical-model record or ADR              |
| 1.2-A   | Every core entity declares a substantial definition, an owner and an escalation; a stale `fp=`          |
| 1.2-C   | CODEOWNERS routes each entity's defining file to its declared owner (last match wins)                   |
| 1.13-A  | Each owner has an ownership record with responsibilities and a decision it can make alone               |
| 1.5-D   | Every join in the query corpus follows a reference, or a shared dimension, between its two sides        |
| 1.6-B   | Three or more thin lookup tables in a chain (`address → city → state → country`)                        |
| 1.7-A   | A scratch-named or scratch-schema model that a durable view, query or table depends on                  |
| 1.9-B   | Shortcut markers (`for now`, TODO) in descriptions name an ADR with Rationale and Cost sections         |
| 2.2-A   | A declared reference across a domain boundary (schema, package, directory) records its mapping          |
| 2.3-A   | A classification (`category`, `region`, `tier`…) stored as free text, not a taxonomy reference          |
| 2.5-A   | Published entities declare a status, describe 90% of columns and give measures a unit or currency       |
| 2.7-A   | A diagnosis, country, currency or similar code names its standard; raised by a second signal (M5)       |
| 2.7-B   | An enum whose values are mostly ISO 3166, ISO 4217 or ISO 639 codes declares the standard and uses it   |
| 2.8-A   | Entities agents may change declare `agent_ops`, matching the grants, and encode status transitions      |
| 3.1-A   | A life-cycle state that needs history has an event entity, versions or a declared stream                |
| 3.2-A   | An operational copy of a referenced entity's field states its reason                                    |
| 3.3-B   | Wide (50+ columns), `obt_` or `layer=obt` tables declare their layer, grain and core `upstream=`        |
| 3.4-A   | Master and reference data rejects bad records: required, quantity, unit and hierarchy checks            |
| 3.5-A   | No ALTER, RENAME, index or trigger on a `source=legacy\|third_party` table after its copied schema      |
| 4.1-A   | Every entity has a primary key, and no key is a mutable value or a bare time                            |
| 4.1-B   | Every Go and JSON Schema form of an entity carries its key, required and of a compatible type           |
| 4.2-A   | An entity fed by several systems declares its identity resolution and its owner                         |
| 4.3-A   | A key renamed since the lock, or rewritten by an UPDATE, without an identity map                        |
| 4.4-A   | Composite primary keys with natural columns; a natural primary key on a multi-source entity             |
| 4.4-B   | Natural and legacy IDs (`legacy_*_id`, `email`, `sku`, `natural_key`) lead an index or unique key       |
| 4.5-A   | A display value of another entity (`customer_name`, `author_email`) with no ID beside it                |
| 4.6-A   | Entities cut out of a file are keyed by source ID and location                                          |
| 4.6-B   | An entity referencing a declared file entity declares `location=` and keys on it with the reference     |
| 4.7-A   | Hash-named primary keys; file entities keyed by name, path or anything but a UUID                       |
| 4.8-A   | Thirty or more attributes, with nullable prefix groups that apply only to some rows                     |
| 4.8-B   | A field named for another entity; a warning when this entity references that one (M5)                   |
| 4.9-A   | A link table's definition names the entities it links, unless it is a `removal_candidate`               |
| 4.10-A  | Entities split by channel or generic noun (`chat_tickets`, `email_tickets`) with matching fields        |
| 4.11-A  | Three or more entities repeating a contact, location, agreement or account cluster                      |
| 4.11-B  | Cluster members, and entities new or redesigned since the lock, record a current `pattern=`             |
| 5.1-A   | PostgreSQL and SQL:2016 reserved words, bare type words, generic and weakly ambiguous names             |
| 5.1-B   | Date and time columns are named `<event>_at\|on\|date\|time\|ts` after an event word                    |
| 5.2-A   | Names follow the dominant case style and table number; no field spelled two ways                        |
| 5.2-B   | Cryptic abbreviations (`qty`, `cust`) and letters-and-a-number names (`c_3`)                            |
| 5.3-A   | Every nullable field says what NULL means                                                               |
| 5.3-B   | `AVG`/`SUM`/`MIN`/`MAX`/`COUNT` of a column whose NULL means zero, or whose NULL is undeclared          |
| 5.3-C   | Linked Go, SQL and JSON Schema fields agree on whether they may be NULL                                 |
| 5.4-A   | Status, type, reason and code fields have an allowed list, CHECK or lookup reference                    |
| 5.4-B   | Every form of a field allows the same codes; no code written twice or as a near-synonym                 |
| 5.5-A   | Numbered sibling columns (`phone1`…`phone3`) and fixed-size Go arrays                                   |
| 6.1-A   | Arrays of references instead of a junction table                                                        |
| 6.2-A   | A column named like a reference with no foreign key or `ref=` declaration                               |
| 6.2-B   | A nullable reference whose cardinality is mandatory or undeclared                                       |
| 6.2-C   | A reference declared in one form is a foreign key, and as required, in the others                       |
| 6.4-A   | A reference with relationship attributes (`spouse_since`); a warning on a self-reference (M5)           |
| 6.4-B   | A junction with attributes of its own, used as an entity, keyed only by its references                  |
| 6.4-C   | A change since the lock that adds `_since`, `_role` or a second side to a reference                     |
| 6.5-B   | Embedded lists named for growth (`history`, `events`) without `maxItems` of at most 100 (a note)        |
| 6.6-A   | Line items keep the master's price or cost; a warning when the master has validity columns              |
| 6.7-A   | Inferred-link entities record a confidence, a method and a review status with a pending state           |
| 7.1-A   | Every entity has a well-formed grain statement whose nouns resolve; a stale `fp=` is reported (M1)      |
| 7.1-B   | The declared grain matches a primary or unique key                                                      |
| 7.1-C   | Upsert keys in the query corpus match the declared grain                                                |
| 7.2-A   | `SUM`, `AVG` or `COUNT(col)` after a join that raises the grain (fan-out, chasm trap)                   |
| 7.3-A   | Plain `INSERT` into an incrementally loaded entity                                                      |
| 7.3-B   | An incrementally loaded entity has a key at its grain or an event key                                   |
| 7.3-D   | Readers deduplicating an entity on its own grain key                                                    |
| 7.4-A   | A child column named after an ancestor; raised when 7.4-C or 7.4-D agrees (M5)                          |
| 7.4-B   | Mixed-grain tables have a `NOT NULL`, enumerated `grain_level`, and aggregates filter on it             |
| 7.4-C   | A child column that repeats a referenced parent's column                                                |
| 7.4-D   | A write that copies an ancestor's field into a child, in SQL or from Go                                 |
| 7.5-B   | Atomic data dropped, truncated or deleted while an aggregate summarizes it                              |
| 7.5-C   | Event tables stored as daily counts                                                                     |
| 7.5-D   | Queries asking for a finer unit than a column holds: by its type, its writes or the grain               |
| 7.6-A   | Agent-exposed entities declare a grain and coarser roll-ups; chunks state size and overlap (a note)     |
| 8.2-A   | A junction between a measure-bearing entity and another declares its `m2m` policy                       |
| 8.3-A   | Every measure declares its additivity class                                                             |
| 8.3-B   | `SUM` of a semi-additive measure across the dimension it is semi-additive over                          |
| 8.4-A   | Stored ratios and averages at a coarse grain without their additive components or a sketch              |
| 8.4-B   | `SUM` or `AVG` of a stored average, ratio or distinct count                                             |
| 8.5-B   | Stored aggregates key on a period and declare `dimensions=` naming key columns                          |
| 8.6-A   | Window outputs declare `window=`, `lateness=`, `late_policy=`; sliding windows have no additive measure |
| 8.8-A   | Identifiers and codes declared as measures; embeddings without a `pooling=` declaration                 |
| 8.9-B   | Key metrics declare `reconciles_with=` an additive measure on another entity, in the same units         |
| 9.1-A   | Event, ingestion and processing time are told apart                                                     |
| 9.1-B   | Aggregations bucketed or filtered by load or processing time while event time exists                    |
| 9.1-C   | Event time a Go or JSON Schema form carries is stored by the SQL form                                   |
| 9.2-A   | Naive timestamps, and dates or times stored as strings or undeclared integers                           |
| 9.2-B   | Local times have a time-zone field constrained to IANA zone names                                       |
| 9.3-B   | Interval end fields declare `end=` and, when nullable, `null=open`; both ends share a type              |
| 9.4-A   | File entities record creation and ingestion times and a dedup basis                                     |
| 9.4-B   | File entities are not deduplicated by file name                                                         |
| 9.8-A   | Event time stored with less precision than its source provides                                          |
| 9.8-B   | Event time truncated on the way to storage (`date_trunc`, `::timestamp(0)`, `Truncate`, `Format`)       |
| 10.1-A  | Audit and load columns say whether they record the business or the system, and describe it              |
| 10.2-A  | Records kept in a JSON column instead of typed columns, or no column for a schema's extension           |
| 10.2-B  | Required or core JSON Schema properties with a union of kinds or no type                                |
| 10.3-A  | File content stored in a row; stored-object references without who, when, what and version              |
| 10.4-A  | One blended sentiment score with no per-aspect scores                                                   |
| 10.6-A  | Agent-action entities keep the prompt, the tools available and the model, all required                  |
| 10.7-A  | Training or agent data comes with model_version, agent_run and tool_call entities and their links       |
| 10.9-A  | Data that may be synthetic carries `is_synthetic`, `generator_version`, `source_dataset_id` and a CHECK |
| 10.10-A | Graph edge endpoints are enforced, required foreign keys that do not SET NULL                           |
| 10.10-B | Relationship types kept as rows, not one table per relationship or a closed enum (a note)               |
| `mml/*` | Parse errors, unknown annotation keys, inline/sidecar conflicts, waivers without a reason, a stale lock |

The heavy lifting is done by libraries focused on each job:

- SQL: PostgreSQL's own grammar, through
  [go-pgquery](https://github.com/wasilibs/go-pgquery), which runs
  libpg_query as WebAssembly, so the binary builds with `CGO_ENABLED=0`.
- Go: [go/packages](https://pkg.go.dev/golang.org/x/tools/go/packages),
  `go/types` and [go/ssa](https://pkg.go.dev/golang.org/x/tools/go/ssa).
- JSON Schema: [go-jsonschema](https://github.com/atombender/go-jsonschema)'s
  schema model. Positions and `x-mml-*` keywords, which it does not keep, come
  from a small reader in `internal/jsonschema`.
- SARIF: [go-sarif](https://github.com/owenrumney/go-sarif), whose schema
  validator also checks the output in the tests.
- Plurals for linking names across sources:
  [inflection](https://github.com/jinzhu/inflection).

### Deviations from rule_plan.md

- **5.1-A** checks lists 1 (PostgreSQL reserved keywords, generated from the
  parser's own `kwlist.h`), 2 (SQL:2016, a warning), 4, 5 and 6. List 3
  (other dialects) waits for a configuration file.
- **7.2-A** does not report `COUNT(*)`. After a join it counts the result's
  own rows, and whether that was intended cannot be told from the query.
- **7.3-A** checks only entities that are loaded incrementally (`layer:
  event|analytical|feature` or `load: incremental`). Seed data in migrations
  would otherwise be reported. It reports writes with no conflict key; a key
  that does not match the grain is reported by 7.1-C.
- **7.5-D** counts a timestamp as truncated only when every write in the
  corpus truncates it.
- **7.4-D** and **9.8-B** read Go values bound to `$n` parameters, sqlx named
  parameters and gorm models. Values passed through more than one wrapper,
  and squirrel query builders, are not traced. A Go struct field is followed
  through at most four other structs.
- **4.3-A** reports an UPDATE of a primary-key column wherever the query
  corpus has one, since whether a migration is newer than the lock cannot be
  told from the files.
- **3.4-A** checks SQL entities only: Go `validate` tags and JSON Schema
  bounds are not read yet.
- **5.1-B** reads a hand-curated `lexicon/event_verbs.txt` rather than
  WordNet's participles, accepts any word ending in `-ed`, extends the
  default event nouns (`sale`, `booking`, `session` and others), and lets
  period bounds (`period_start`, `valid_to`) pass, since 8.5-B and 9.3-B ask
  for those names. A name that 5.1-A already reports is left to it.
- **5.3-B** checks aggregates only; arithmetic on a nullable operand needs
  expressions the query model does not keep.
- **1.5-D** also sanctions references inferred from a name (6.2-A reports
  them as undeclared) and joins through a shared dimension. It does not read
  `model/conceptual.yaml` or `-- mml:join` comments, so a deliberate join on
  other columns needs a `waive`.
- **1.6-B** uses the default chain length of 3; the translation-table case
  waits for a `locales` setting.
- **1.2-C** matches `team:x` and `group:x` to any `@org/x` and skips `role:`
  owners, since `mml.yaml` has no `owner_aliases` yet.
- **4.1-B** leaves unlinked event and document schemas to 4.1-A, and treats
  a UUID and a string as the same key type.
- **6.2-C** asks only a SQL form to declare the foreign key itself; a Go or
  JSON Schema form is asked only to be as required.
- **8.3-B** counts any `WHERE` test of the dimension as pinning it, and does
  not check window aggregates. **8.4-B** checks stored fields only, not CTE or
  view outputs.
- **9.1-C** compares event time only; it does not read Go tags that map an
  event field onto an ingestion column.
- **9.2-B** trusts a `local_` name only on a date or time field.
- **2.2-A** takes domains from a `domain` annotation, the SQL schema
  (`public` is none), the Go package or the JSON Schema directory, until
  `mml.yaml` has `domains.<name>.paths`. A mapping is recorded on the
  referencing field (`mapping=`), not in a sidecar `boundaries:` list.
- **2.5-A** leaves the definition and owner to 1.2-A, which asks the same
  entities for them. Its thresholds (four words, 90% of columns) are fixed.
  Views are not checked.
- **2.7-A** has no regulated-domain signal, which needs `domains[].regulated`.
  **2.7-B**'s ISO lists are generated from `golang.org/x/text` by
  `internal/tools/genvaluesets`; codes ISO has withdrawn but CLDR still knows
  (`YU`, `SU`) count as standard.
- **2.8-A** knows an agent's database role by its name (`agent`, `llm`, `ai`,
  `bot`…) rather than an `agents.roles` setting, and does not compare a
  transitions table's values with the status enum, which needs seed rows.
- **10.1-A** counts only names in its metadata lexicon (`updated_at`,
  `_loaded_at`, `batch_id`…); rule_plan.md also counts every other temporal
  field, which §9.1 already asks to declare its kind.
- **10.2-A** reads an extension point from `x-mml-extension` only, not from
  `additionalProperties`. **10.2-B** leaves `oneOf` and `anyOf` alone, since
  their branches are not loaded.
- **10.3-A** treats `file_url`, `image_key`, `object_key` and similar as
  references to stored objects, not any `_url`, so a `website_url` is not one.
- **10.7-A** takes its signals from the model alone (training or feature
  layers, agent-action entities); MLflow, wandb, model files and SDK imports
  are not read.
- **10.9-A** asks for the CHECK in SQL only; a JSON Schema `if`/`then` is not
  read.
- **1.9-B** reads a `deviation=` annotation naming an ADR by path or file
  name, inline or in the sidecar, until `mml.yaml` has a `deviations:` list.
  An issue reference alone does not count. Scratch work is not checked.
- **3.3-B** uses a fixed 50-column threshold. **3.5-A** treats statements in
  the file that creates a vendor table as part of the copied schema, does not
  report a `DROP` (the entity is gone), and knows foreign tables by
  `source=legacy|third_party` rather than `sources.foreign_schemas`.
- **4.4-A** counts line ordinals (`line_no`, `seq`, `position`), `valid_from`,
  `version` and dates as positions or periods rather than natural values, and
  exempts stored aggregates (`layer=analytical|obt|feature`, `dimensions=`, or
  a grain per calendar period). In Go and JSON Schema a surrogate key is
  known by its name. Design C's fan-out count is part of 4.4-A's message.
  **4.4-B** checks SQL tables only, and not natural keys dropped since the
  lock.
- **4.5-A** knows roles (reviewer, author, uploader, owner) from a fixed list;
  `refers=` names the entity a role stands for.
- **4.7-A** has no `allow_identity` setting, so a file entity keyed by an
  integer is reported.
- **4.8-A** groups sparse fields by name prefix only, not by a CHECK tying
  them to a discriminator. **4.8-B** leaves copies of a parent's field to
  3.2-A and does not yet report unnamed foreign groups.
- **4.9-A** checks that an existing definition names what the link table
  links; a missing, thin or stale definition is 1.2-A's.
- **4.10-A** has two of its three signals: the UNION-back query (Design B)
  is not read yet.
- **4.11-A** keeps its clusters in code rather than
  `lexicon/pattern_clusters.yaml`. **4.11-B** does not report repeated
  identical reasons.
- **5.2-A** judges SQL and JSON Schema names, not Go's, and says nothing when
  no style dominates. **5.2-B** knows cryptic tokens from a bundled table,
  `lexicon/abbreviations.txt`, not an English dictionary, and does not split
  run-together words (`orderdate`).
- **5.4-A** skips Go fields, whose const blocks are not loaded, and
  lookup-table keys, and leaves classifications and regulated codes to 2.3-A
  and 2.7-A. **5.4-B** counts inserted letters, not changed ones, as a
  near-synonym: `canceled` and `cancelled`, but not `shipped` and `skipped`.
- **6.4-A** treats only a self-reference as a peer. **6.4-B** has no glossary
  exemption.
- **6.5-A** waits for inline object shapes, which no loader keeps. **6.5-B**
  reads growth from a list's name only, not from its items' timestamps.
- **6.6-A** confirms that a price changes from validity columns only; the
  update-path signal needs the Phase 2 query check.
- **7.6-A** checks roll-ups against time units and entity names; comparing
  them with the aggregates derived from the entity needs lineage.
- **8.8-A** shares its list of identifier and code names with 8.3-A, which
  now accepts `measure=nominal`.
- **8.9-B** does not check that the target sits upstream at a finer grain.
- **`annotate`** prints stubs rather than editing files, and has no
  `--refresh` yet.
- **`mml.lock.json`** does not record the seed rows of reference entities.

## Not yet implemented

- Every other design in rule_plan.md.
- The H-family designs beyond 1.1-B, 4.3-A and 7.5-B.
- M4 result ingestion (`--results`) and the `scaffold` command, which go
  together: a test result is mapped to a rule through the metadata
  `scaffold` writes when it generates the test.
- The rest of the artifact corpus: glossary, dbt and semantic-layer files.
- The `catalog pull` command, and a second SQL dialect.

## Development

```sh
just test      # unit, fixture and end-to-end suites
just lint      # golangci-lint
just example   # dogfood: _example must produce no findings
just mutate    # mutation-test the pure core
just generate  # regenerate the keyword list and explain's text
```

The layering is enforced by depguard in `.golangci.yaml`:

- `internal/mixedmodellint` is the pure core. It never touches the filesystem,
  the clock or a parser.
- The adapters, each named after the library or tool it wraps
  (`internal/postgres`, `internal/gopackages`, `internal/jsonschema`,
  `internal/sidecar`, `internal/sarif` and `internal/git`), turn sources
  into core values or core values into output. `internal/git` is the only
  package that runs `git`.
- `internal/lint` composes the adapters.
- The commands (`cmd/check`, `cmd/lock`, `cmd/annotate`, `cmd/explain`)
  dispatch to `internal/lint` and the core.
