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

| Source      | Files                                                     | Entities                                                                                                                                                                                                                                                                                                                                                                                                  |
| ----------- | --------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| SQL         | `*.sql`, replayed in path order                           | `CREATE TABLE` and `CREATE VIEW` after every `ALTER` and `DROP`. Other statements form the query corpus.                                                                                                                                                                                                                                                                                                  |
| Go          | Each Go module (a `go.mod`), loaded with type information | Structs with a storage tag (`db`, `gorm`, `bun`, `sql`, `bson`, `parquet`, `avro`), an `mml` tag, or an `//mml:` directive, including fields embedded from other packages. A struct with only `json` tags is a payload, not stored data. SQL passed to `database/sql`, pgx, sqlx or gorm, and to any interface method named like a `database/sql` method (such as sqlc's `DBTX`), joins the query corpus. |
| JSON Schema | `*.schema.json`, or JSON with `"$schema"`                 | The root object and each object in `$defs` or `definitions`                                                                                                                                                                                                                                                                                                                                               |
| Sidecar     | `mml.yaml` at the root                                    | Annotations for any entity                                                                                                                                                                                                                                                                                                                                                                                |

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

[sqlc](https://sqlc.dev) keeps each query in a `.sql` file and copies it into
generated Go. A statement in a `.sql` file and a Go constant with the same
`-- name:` header are read as one query, positioned in the `.sql` file, so
findings about it are not reported twice.

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

| Design  | Checks                                                                                               |
| ------- | ---------------------------------------------------------------------------------------------------- |
| 5.1-A   | Reserved words, bare type words (`date`, `timestamp`), generic and weakly ambiguous column names     |
| 5.5-A   | Numbered sibling columns (`phone1`…`phone3`) and fixed-size Go arrays                                |
| 7.1-A   | Every entity has a well-formed grain statement whose nouns resolve; a stale `fp=` is reported (M1)   |
| 7.1-B   | The declared grain matches a primary or unique key                                                   |
| 7.1-C   | Upsert keys in the query corpus match the declared grain                                             |
| 7.2-A   | `SUM`, `AVG` or `COUNT(col)` after a join that raises the grain (fan-out, chasm trap)                |
| 7.3-A   | Plain `INSERT` into an incrementally loaded entity                                                   |
| 7.3-B   | An incrementally loaded entity has a key at its grain or an event key                                |
| 7.3-D   | Readers deduplicating an entity on its own grain key                                                 |
| 7.4-B   | Mixed-grain tables have a `NOT NULL`, enumerated `grain_level`, and aggregates filter on it          |
| 7.4-C   | A child column that repeats a referenced parent's column                                             |
| 7.4-D   | A write that copies an ancestor's field into a child, in SQL or from Go through sqlc-style params    |
| 7.5-C   | Event tables stored as daily counts                                                                  |
| 7.5-D   | Queries asking a column for a finer unit than it holds: a date, or a timestamp every write truncates |
| 8.4-A   | Stored ratios and averages at a coarse grain without their additive components or a sketch           |
| 9.2-A   | Naive timestamps, and dates or times stored as strings or undeclared integers                        |
| 9.8-B   | Event time truncated on the way to storage (`date_trunc`, `::timestamp(0)`, `Truncate`, `Format`)    |
| `mml/*` | Parse errors, unknown annotation keys, inline/sidecar conflicts, waivers without a reason            |

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

- **5.1-A** checks lists 1 (PostgreSQL 17 reserved keywords), 4, 5 and 6. It
  does not yet check list 2 (SQL:2016) or list 3 (other dialects). The
  keyword list was copied by hand from `kwlist.h`, not generated from it.
- **7.2-A** does not report `COUNT(*)`. After a join it counts the result's
  own rows, and whether that was intended cannot be told from the query.
- **7.3-A** checks only entities that are loaded incrementally (`layer:
  event|analytical|feature` or `load: incremental`). Seed data in migrations
  would otherwise be reported. It reports writes with no conflict key; a key
  that does not match the grain is reported by 7.1-C.
- **7.5-D** takes a column's unit from its type (`date`) or from its writes:
  a timestamp counts as truncated only when every write in the corpus
  truncates it. The declared grain's time unit is not used yet.
- **7.4-D** and **9.8-B** read Go values only where they are bound to `$n`
  parameters as separate arguments (`db.ExecContext(ctx, q, a, b)`,
  including sqlc's generated methods). Named parameters (`sqlx.NamedExec`),
  gorm models, and values passed through `args...` are not traced. A Go
  struct field is followed through at most four other structs. 7.4-D does
  not yet raise 7.4-A or 7.4-C under M5, since 7.4-A is not implemented and
  7.4-C is not heuristic.

## Not yet implemented

- Every other design in rule_plan.md.
- The H-family designs and `--base` diff mode.
- The M3 lock file, and M4 result ingestion (`--results`).
- The artifact corpus: CODEOWNERS, glossary, dbt and semantic layer files.
- The rest of the Go code corpus: named-parameter and ORM writes, other
  time handling from SSA (§9.x), and squirrel query builders.
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
- The adapters, each named after the library it wraps (`internal/postgres`,
  `internal/gopackages`, `internal/jsonschema`, `internal/sidecar` and
  `internal/sarif`), turn sources into core values or core values into
  output.
- `internal/lint` composes the adapters.
- `cmd/check` dispatches to `internal/lint`.
