# CLAUDE.md

## Overview

Kanly is a Go mutation tester that uses Mutant Schema Generation (MSG): all mutants for a package are encoded into a single rewritten source tree, compiled once into a test binary, and then each mutant is activated at runtime via the `KANLY_MUTANT` environment variable — one compile, many test runs.

## Package map

- **`cmd/kanly`** — CLI entry point. Parses `--format`, `--timeout`, `--diff`/`--diff-base`, `--tests` (test-name regex pre-narrowing the baseline inventory), and `--mutant` (comma-separated schema IDs to re-run). Positional args take the form `<pkg-pattern>[:<func-list>]` and are parsed via `internal/selector`. Loads packages via `source.LoadAll`, composes a diff × function-name filter, then orchestrates per-package: rewrite → compile → baseline → per-test coverage → mutant loop → aggregate → report. When any scope is active the returned inventory is narrowed to tests covering at least one kept mutation, so `zero_kill_tests` and redundant-group analysis reflect the focused scope. See `cmd/kanly/main.go`.
- **`internal/source`** — thin wrapper around `golang.org/x/tools/go/packages.Load`; returns a `Package` with typed AST and type-checker info. `Load(dir)` loads a single directory; `LoadAll(workDir, patterns...)` supports Go-style patterns including `./...`. See `internal/source/loader.go:15`.
- **`internal/selector`** — parses positional CLI args of the form `<pkg-pattern>[:<func-list>]` into `Spec`s and matches user-supplied function names against the canonical `funcDeclName` form (`Foo`, `(T).Bar`, `(*T).Bar`). Matching is lenient: `Server.Handle` matches both pointer and value receivers; `*T.Bar` normalises to `(*T).Bar`. Glob patterns (`./...`) cannot carry a function filter. See `internal/selector/selector.go:26`.
- **`internal/mutation`** — core types: `Operator` interface, `Candidate`, `Mutation`, `Result`, and the global operator registry (`Register`/`Operators`). See `internal/mutation/operator.go:17` and `internal/mutation/types.go:17`.
- **`internal/operators`** — concrete operator implementations; each file registers itself via `init()` in `internal/operators/register.go:5`. Currently contains `IntArith` (`int_arith`: `+↔-`, `*↔/`, `%→*`), `IntCmpBoundary` (`int_cmp_boundary`: `<↔<=`, `>↔>=`), `IntCmpNegate` (`int_cmp_negate`: six comparison flips), `BoolLogic` (`bool_logic`: `&&↔||`), `BoolNot` (`bool_not`: `!→⌀`), `ErrReturnNil` (`err_return_nil`: `err→nil`), `CallDelete` (`call_delete`: `f(...)→⌀`), `StringLiteral` (`string_literal`: non-empty `"x"`→`""`), `SliceIndex` (`slice_index`: `a[i]→a[i±1]`), `IncDec` (`inc_dec`: `x++↔x--`), `IntCompoundAssign` (`int_compound_assign`: compound-op flips), `IntBitwise` (`int_bitwise`: `&↔|`, `^→&`, `<<↔>>`, `&^→&`), `IntLiteral` (`int_literal`: `n→0,1,-n,n±1`), `ReturnZero` (`return_zero`: `int→0`, `string→""`, `bool→false`, ptr/slice/map/chan/func/iface→`nil`), `BoolLiteral` (`bool_literal`: `true↔false` for predeclared keywords whose contextual type is exactly `bool`), and `StructFieldZero` (`struct_field_zero`: keyed struct-literal field value → zero of T; shares the `return_zero` dispatcher). Shared type guards live in `internal/operators/typecheck.go`.
- **`internal/schema`** — AST rewriter that produces the mutant schema (one file per source file, plus a dispatcher `init()` file). The `Rewrite` filter has signature `func(file string, line int, funcName string) bool` so callers can scope mutants by line range (diff) AND enclosing function in one predicate. `FuncNames(pkg)` exposes the package's canonical function names for CLI validation and "did you mean" hints. See `internal/schema/rewriter.go:108` and `internal/schema/template.go:3`.
- **`internal/runner`** — assembles the overlay JSON, calls `go test -c` to compile the schema binary, and executes it once per mutant ID. `RunBaseline` accepts an optional `testRunRegex` so callers can pre-narrow which tests participate in the baseline + per-test coverage pass (used by `--tests`). See `internal/runner/runner.go:139`.
- **`internal/report`** — aggregates `[]mutation.Result` into a `Report` + `Summary`, then renders as plain text or JSON. Exposes a top-level `Scope` field (`omitempty`) describing the active filters; text output prefixes a `Scope:` header line. See `internal/report/report.go:33`.
- **`internal/e2e`** — integration test that builds the CLI binary and runs it against `internal/runner/testdata/sample`, validating the JSON output against a pinned ledger. See `internal/e2e/e2e_test.go:25`.

## Build & test

```bash
# Build the CLI
go build ./cmd/kanly

# Run everything (slow — includes the e2e binary build)
go test ./...

# Unit tests only (skips e2e and other slow tests)
go test -short ./...

# Regenerate the JSON golden file for internal/report
GOLDEN_UPDATE=1 go test ./internal/report
```

## Conventions

- **Table-driven tests** using the stdlib `testing` package only — no testify or other assertion libraries.
- **`relDir` helper** converts a path relative to the test file's source location to an absolute path. Used in `cmd/kanly/main_test.go:12`, `internal/e2e/e2e_test.go:12`, and `internal/report/report_test.go:17`.
- **Golden JSON files** live under `testdata/` next to the test file that owns them (e.g. `internal/report/testdata/golden.json`). Regenerate with `GOLDEN_UPDATE=1`.
- **Commit prefix:** `[task N]` where N is the 1-based line index of the task in `.claude/plan.md` (counting all task lines across the whole file). When no plan file exists, use `task: `.

## Pitfalls

### Do NOT use `.Underlying()` when matching int types

```go
// WRONG — passes named types like "type MyInt int"
if _, ok := lv.Type.Underlying().(*types.Basic); ok { ... }

// CORRECT — exact identity check only
if lv.Type != types.Typ[types.Int] { return true }
```

Named types like `type MyInt int` must be excluded. All three operators share the `intOperands` helper in `internal/operators/typecheck.go` which performs this exact identity check. See `internal/operators/arithmetic.go` and `internal/operators/comparison.go`.
