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
mixedmodellint check [--dir DIR] [--format text|json|sarif] [--output FILE]
                     [--baseline FILE] [--write-baseline FILE]
```

- The exit status is 1 when any finding is an error. A `[MUST]` rule reports
  errors, a `[SHOULD]` rule warnings, and a `[CONSIDER]` rule notes. A
  heuristic design reports one level lower.
- `--format sarif` writes SARIF 2.1.0 for GitHub code scanning.
- To adopt the linter on an existing code base, run `--write-baseline
  mml-baseline.json` once. Then pass `--baseline mml-baseline.json`, so that
  only new findings fail.

### What is read

| Source      | Files                                     | Entities                                                                                                                                                                                  |
| ----------- | ----------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| SQL         | `*.sql`, replayed in path order           | `CREATE TABLE` and `CREATE VIEW` after every `ALTER` and `DROP`. Other statements form the query corpus.                                                                                  |
| Go          | `*.go` except tests                       | Structs with a storage tag (`db`, `gorm`, `bun`, `sql`, `bson`, `parquet`, `avro`), an `mml` tag, or an `//mml:` directive. A struct with only `json` tags is a payload, not stored data. |
| JSON Schema | `*.schema.json`, or JSON with `"$schema"` | The root object and each object in `$defs` or `definitions`                                                                                                                               |
| Sidecar     | `mml.yaml` at the root                    | Annotations for any entity                                                                                                                                                                |

The walk skips hidden directories and directories whose names start with
`_`. It also skips `vendor`, `node_modules`, `testdata`, `bin`, `dist` and
`target`.

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

This release implements the foundation of rule_plan.md and the designs below.
The remaining designs are listed under [Not yet implemented](#not-yet-implemented).

| Design  | Checks                                                                                             |
| ------- | -------------------------------------------------------------------------------------------------- |
| 5.1-A   | Reserved words, bare type words (`date`, `timestamp`), generic and weakly ambiguous column names   |
| 5.5-A   | Numbered sibling columns (`phone1`…`phone3`) and fixed-size Go arrays                              |
| 7.1-A   | Every entity has a well-formed grain statement whose nouns resolve; a stale `fp=` is reported (M1) |
| 7.1-B   | The declared grain matches a primary or unique key                                                 |
| 7.1-C   | Upsert keys in the query corpus match the declared grain                                           |
| 7.2-A   | `SUM`, `AVG` or `COUNT(col)` after a join that raises the grain (fan-out, chasm trap)              |
| 7.3-A   | Plain `INSERT` into an incrementally loaded entity                                                 |
| 7.3-B   | An incrementally loaded entity has a key at its grain or an event key                              |
| 7.3-D   | Readers deduplicating an entity on its own grain key                                               |
| 7.4-B   | Mixed-grain tables have a `NOT NULL`, enumerated `grain_level`, and aggregates filter on it        |
| 7.4-C   | A child column that repeats a referenced parent's column                                           |
| 7.5-C   | Event tables stored as daily counts                                                                |
| 7.5-D   | Queries asking a date column for a finer unit (`date_trunc('hour', d)`)                            |
| 8.4-A   | Stored ratios and averages at a coarse grain without their additive components or a sketch         |
| 9.2-A   | Naive timestamps, and dates or times stored as strings or undeclared integers                      |
| `mml/*` | Parse errors, unknown annotation keys, inline/sidecar conflicts, waivers without a reason          |

The SQL parser is PostgreSQL's own grammar, through
[go-pgquery](https://github.com/wasilibs/go-pgquery), which runs libpg_query
as WebAssembly. The binary therefore builds with `CGO_ENABLED=0`.

### Deviations from rule_plan.md

- **5.1-A** checks lists 1 (PostgreSQL 17 reserved keywords), 4, 5 and 6. It
  does not yet check list 2 (SQL:2016) or list 3 (other dialects). The
  keyword list was copied by hand from `kwlist.h`, not generated from it.
- **7.2-A** does not report `COUNT(*)`. After a join it counts the result's
  own rows, and whether that was intended cannot be told from the query.
- **7.3-A** checks only entities that are loaded incrementally (`layer:
  event|analytical|feature` or `load: incremental`). Seed data in migrations
  would otherwise be reported. It reports writes with no conflict key; a key
  that does not match the grain is reported by 7.1-C.
- **7.5-D** checks the time unit of `date` columns. It does not yet check
  timestamps that were truncated when written (the §9.8-B half).

## Not yet implemented

- Every other design in rule_plan.md.
- The H-family designs and `--base` diff mode.
- The M3 lock file, and M4 result ingestion (`--results`).
- The artifact corpus: CODEOWNERS, glossary, dbt and semantic layer files.
- The Go code corpus (`go/ssa`).
- The `explain`, `scaffold`, `annotate`, `lock` and `catalog pull` commands.

## Development

```sh
just test      # unit, fixture and end-to-end suites
just lint      # golangci-lint
just example   # dogfood: _example must produce no findings
just mutate    # mutation-test the pure core
```

The layering is enforced by depguard in `.golangci.yaml`:

- `internal/mixedmodellint` is the pure core. It never touches the filesystem,
  the clock or a parser.
- The adapters (`internal/postgres`, `internal/goast`, `internal/jsonschema`
  and `internal/sidecar`) turn bytes into core values.
- `internal/lint` composes the adapters.
- `cmd/check` dispatches to `internal/lint`.
