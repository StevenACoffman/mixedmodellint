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
| SQL         | `*.sql`, replayed in path order                           | `CREATE TABLE` and `CREATE VIEW` after every `ALTER` and `DROP`. Other statements form the query corpus.                                                                                                                                                                                                                                                                                                  |
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

| Artifact          | Files                                                    | Read by       |
| ----------------- | -------------------------------------------------------- | ------------- |
| Decision records  | `docs/adr/*.md`                                          | 1.1-B         |
| Logical model     | `model/logical/*.yaml` (`entity`, `attributes`, `keys`)  | 1.1-B         |
| Ownership records | `model/ownership/<owner>.yaml`, or `owners:` in mml.yaml | 1.13-A, 4.2-A |
| CODEOWNERS        | `CODEOWNERS`, `.github/CODEOWNERS`, `docs/CODEOWNERS`    | 1.2-A, 4.2-A  |

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
| 3.1-A   | A life-cycle state that needs history has an event entity, versions or a declared stream                |
| 3.2-A   | An operational copy of a referenced entity's field states its reason                                    |
| 3.4-A   | Master and reference data rejects bad records: required, quantity, unit and hierarchy checks            |
| 4.1-A   | Every entity has a primary key, and no key is a mutable value or a bare time                            |
| 4.1-B   | Every Go and JSON Schema form of an entity carries its key, required and of a compatible type           |
| 4.2-A   | An entity fed by several systems declares its identity resolution and its owner                         |
| 4.3-A   | A key renamed since the lock, or rewritten by an UPDATE, without an identity map                        |
| 4.6-A   | Entities cut out of a file are keyed by source ID and location                                          |
| 4.6-B   | An entity referencing a declared file entity declares `location=` and keys on it with the reference     |
| 5.1-A   | PostgreSQL and SQL:2016 reserved words, bare type words, generic and weakly ambiguous names             |
| 5.1-B   | Date and time columns are named `<event>_at\|on\|date\|time\|ts` after an event word                    |
| 5.3-A   | Every nullable field says what NULL means                                                               |
| 5.3-B   | `AVG`/`SUM`/`MIN`/`MAX`/`COUNT` of a column whose NULL means zero, or whose NULL is undeclared          |
| 5.3-C   | Linked Go, SQL and JSON Schema fields agree on whether they may be NULL                                 |
| 5.5-A   | Numbered sibling columns (`phone1`…`phone3`) and fixed-size Go arrays                                   |
| 6.1-A   | Arrays of references instead of a junction table                                                        |
| 6.2-A   | A column named like a reference with no foreign key or `ref=` declaration                               |
| 6.2-B   | A nullable reference whose cardinality is mandatory or undeclared                                       |
| 6.2-C   | A reference declared in one form is a foreign key, and as required, in the others                       |
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
| 8.2-A   | A junction between a measure-bearing entity and another declares its `m2m` policy                       |
| 8.3-A   | Every measure declares its additivity class                                                             |
| 8.3-B   | `SUM` of a semi-additive measure across the dimension it is semi-additive over                          |
| 8.4-A   | Stored ratios and averages at a coarse grain without their additive components or a sketch              |
| 8.4-B   | `SUM` or `AVG` of a stored average, ratio or distinct count                                             |
| 8.5-B   | Stored aggregates key on a period and declare `dimensions=` naming key columns                          |
| 8.6-A   | Window outputs declare `window=`, `lateness=`, `late_policy=`; sliding windows have no additive measure |
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
