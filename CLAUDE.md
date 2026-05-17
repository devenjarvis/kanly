# CLAUDE.md

## Overview

Kanly is a Go mutation tester that uses Mutant Schema Generation (MSG): all mutants for a package are encoded into a single rewritten source tree, compiled once into a test binary, and then each mutant is activated at runtime via the `KANLY_MUTANT` environment variable — one compile, many test runs.

## Package map

- **`cmd/kanly`** — CLI entry point; parses `--format` and `--timeout` flags, expands `./...` or multiple positional args via `source.LoadAll`, then orchestrates per-package: rewrite → compile → run → aggregate → report.
- **`internal/source`** — thin wrapper around `golang.org/x/tools/go/packages.Load`; returns a `Package` with typed AST and type-checker info. `Load(dir)` loads a single directory; `LoadAll(workDir, patterns...)` supports Go-style patterns including `./...`. See `internal/source/loader.go:21`.
- **`internal/mutation`** — core types: `Operator` interface, `Candidate`, `Mutation`, `Result`, and the global operator registry (`Register`/`Operators`). See `internal/mutation/operator.go:17` and `internal/mutation/types.go:17`.
- **`internal/operators`** — concrete operator implementations; each file registers itself via `init()` in `internal/operators/register.go:5`. Currently contains `IntArith` (`int_arith`: `+↔-`, `*↔/`, `%→*`), `IntCmpBoundary` (`int_cmp_boundary`: `<↔<=`, `>↔>=`), and `IntCmpNegate` (`int_cmp_negate`: six comparison flips). Shared type guard lives in `internal/operators/typecheck.go`.
- **`internal/schema`** — AST rewriter that produces the mutant schema (one file per source file, plus a dispatcher `init()` file). See `internal/schema/rewriter.go:23` and `internal/schema/template.go:3`.
- **`internal/runner`** — assembles the overlay JSON, calls `go test -c` to compile the schema binary, and executes it once per mutant ID. See `internal/runner/runner.go:18`.
- **`internal/report`** — aggregates `[]mutation.Result` into a `Report` + `Summary`, then renders as plain text or JSON. See `internal/report/report.go:26`.
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
