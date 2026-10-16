# CLAUDE.md

## Overview

Kanly is a Go mutation tester that uses Mutant Schema Generation (MSG): all mutants for a package are encoded into a single rewritten source tree, compiled once into a test binary, and then each mutant is activated at runtime via the `KANLY_MUTANT` environment variable — one compile, many test runs.

## Package map

- **`cmd/kanly`** — CLI entry point. Parses `--format` (`text|json|llm|sarif`), `--min-score` (exit 3 below it, on `score_with_uncovered`), `--timeout` / `--timeout-factor` (per-mutant timeouts default to factor × the covering tests' baseline time via `runner.Baseline.MutantTimeout`), `--diff`/`--diff-base`, `--tests` (test-name regex pre-narrowing the baseline inventory), `--mutant` (comma-separated schema IDs to re-run), `--operators`/`--skip-operators`/`--exclude`, `--tags`/`--short`, `--verify-kills`, `--fail-fast` (adds `-test.failfast` to mutant runs only, via `runConfig.mutantArgs`; sets `Report.FailFast` and drops the per-test views, since kill sets are partial) and `--quiet` (progress goes to stderr otherwise). Positional args take the form `<pkg-pattern>[:<func-list>]` and are parsed via `internal/selector`. Loads packages via `source.LoadAll`, composes a diff × function-name filter, then orchestrates per-package: read `.kanlyignore` → rewrite → compile → baseline → per-test coverage → mutant loop → confirm → aggregate → report. `establishBaseline` excludes tests that fail unmutated (classified by re-running them alone on the unmodified and the schema binary) and re-runs with `-test.skip`; tests that fail alone in the coverage pass are excluded too; both land in `report.ExcludedTest`. `confirmVerdicts` re-runs every timeout alone with twice the budget, and with `--verify-kills` every kill. A `runner.DirSnapshot` of the package dir moves files created during the run to a quarantine dir (never deletes them) and warns about modified ones. Every covering test runs against each mutant — never narrow that set by hit counts, which changes verdicts. A package that fails at any stage is recorded as a `report.PackageError` (stage tagged via `stageError`) and the run continues; the exit code is 1 if any package was not tested. When any scope is active the returned inventory is narrowed to tests covering at least one kept mutation, so `zero_kill_tests` and redundant-group analysis reflect the focused scope. See `cmd/kanly/main.go`.
- **`internal/source`** — thin wrapper around `golang.org/x/tools/go/packages.Load`; returns a `Package` with typed AST and type-checker info. `Load(dir)` loads a single directory; `LoadAll(workDir, patterns...)` (and `LoadAllTags` with build tags) supports Go-style patterns including `./...` and returns packages that fail to load as `[]LoadError` instead of failing the whole load. See `internal/source/loader.go:15`.
- **`internal/selector`** — parses positional CLI args of the form `<pkg-pattern>[:<func-list>]` into `Spec`s and matches user-supplied function names against the canonical `funcDeclName` form (`Foo`, `(T).Bar`, `(*T).Bar`). Matching is lenient: `Server.Handle` matches both pointer and value receivers; `*T.Bar` normalises to `(*T).Bar`. Glob patterns (`./...`) cannot carry a function filter. See `internal/selector/selector.go:26`.
- **`internal/mutation`** — core types: `Operator` interface, `Candidate`, `Mutation`, `Result`, and the global operator registry (`Register`/`Operators`). See `internal/mutation/operator.go:17` and `internal/mutation/types.go:17`.
- **`internal/operators`** — concrete operator implementations; each file registers itself via `init()` in `internal/operators/register.go:5`. Currently contains `IntArith` (`int_arith`: `+↔-`, `*↔/`, `%→*`), `IntCmpBoundary` (`int_cmp_boundary`: `<↔<=`, `>↔>=`), `IntCmpNegate` (`int_cmp_negate`: six comparison flips), `BoolLogic` (`bool_logic`: `&&↔||`), `BoolNot` (`bool_not`: `!→⌀`), `ErrReturnNil` (`err_return_nil`: `err→nil`), `CallDelete` (`call_delete`: `f(...)→⌀`), `StringLiteral` (`string_literal`: non-empty `"x"`→`""`), `SliceIndex` (`slice_index`: `a[i]→a[i±1]`), `SliceRange` (`slice_range`: `s[lo:hi]→s[lo±1:hi]`/`s[lo:hi±1]` and three-index `:max` bounds; shares the `slice_index` dispatcher), `IncDec` (`inc_dec`: `x++↔x--`), `IntCompoundAssign` (`int_compound_assign`: arithmetic + bitwise compound-op flips — `+=↔-=`, `*=↔/=`, `%=→*=`, `&=↔|=`, `^=→&=`, `<<=↔>>=`, `&^=→&=`), `IntBitwise` (`int_bitwise`: `&↔|`, `^→&`, `<<↔>>`, `&^→&`), `IntLiteral` (`int_literal`: `n→0,1,-n,n±1` — plain `int` contexts only), `ReturnZero` (`return_zero`: `int→0`, `string→""`, `bool→false`, ptr/slice/map/chan/func/iface→`nil`), `BoolLiteral` (`bool_literal`: `true↔false` for predeclared keywords whose contextual type is exactly `bool`), and `StructFieldZero` (`struct_field_zero`: keyed struct-literal field value → zero of T; shares the `return_zero` dispatcher). Shared type guards live in `internal/operators/typecheck.go`; type-bearing dispatchers in `internal/schema/template.go` are generic so the integer/boolean operators fire on sized predeclared types (`int32`, `uint8`, …) and named wrappers (`type UserID int64`, `time.Duration`, `type MyBool bool`).
- **`internal/schema`** — AST rewriter that produces the mutant schema (one file per source file, plus a generated `kanly_schema.go` dispatcher file). `RewriteWith(pkg, ops, Options{Filter, Operators})` is the full form of `Rewrite`; both skip generated files and drop candidates suppressed by `//kanly:ignore` directives (`internal/schema/ignore.go`) or accepted by fingerprint (`Options.Accepted`, read from the package's `.kanlyignore` by `ReadIgnoreFile` in `internal/schema/ignorefile.go`), counted in `Rewritten.Ignored`; accepted fingerprints matching no candidate (in scope or not) come back in `Rewritten.StaleAccepted`. Every mutation carries a `Fingerprint` (hash of function, operator, change, trimmed line text, column within the line and an occurrence number) that survives edits elsewhere in the package; rewriting reads each file from disk to compute it. Filtering, operator selection and suppression all happen after numbering so IDs never shift. The `Rewrite` filter has signature `func(file string, line int, funcName string) bool` so callers can scope mutants by line range (diff) AND enclosing function in one predicate. `FuncNames(pkg)` exposes the package's canonical function names for CLI validation and "did you mean" hints; `FuncRanges(pkg)` returns each canonical name's source `{file, startLine, endLine}` for the LLM renderer's snippet extraction. See `internal/schema/rewriter.go:108` and `internal/schema/template.go:3`.
- **`internal/runner`** — assembles the overlay JSON, calls `go test -c` to compile the schema binary, and executes it once per mutant ID. Every test-binary run goes through `testCommand`/`runTestCommand` (`proc.go`): own process group killed on timeout, `WaitDelay` so subprocesses holding the output pipe cannot stall a run, and a private `TMPDIR`. Failed runs return `*TestFailure` (failed top-level tests + output); `RunTests` re-runs named tests; `CollectPerTestCoverage` returns tests that fail alone instead of erroring. Compile functions take trailing build flags and run functions trailing test-binary args. `RunBaseline` returns a `Baseline` (tests, per-test durations parsed from `-test.v`, wall time) and accepts an optional `testRunRegex` so callers can pre-narrow which tests participate in the baseline + per-test coverage pass (used by `--tests`). See `internal/runner/runner.go:139`.
- **`internal/report`** — aggregates `[]mutation.Result` into a `Report` + `Summary`, then renders as plain text, JSON, or `llm` (Markdown artifact for prompting an LLM with surviving mutants + redundant/zero-kill tests). The LLM renderer takes an `LLMSource` carrying `FuncRanges` (per-package canonical-name → source range, from `schema.FuncRanges`) and a `ReadFile` (defaults to `os.ReadFile`; faked in tests) so it can slice the enclosing function source out for each survivor. Structure follows arXiv:2506.02954: a `## Task` preamble framing mutation score (not coverage) as the goal with three outcomes per mutant (test it, simplify the code, accept it in `.kanlyignore`) and a warning against change-detector tests, separate `## Live mutants` (status=survived) and `## Uncovered mutants` (status=not_covered) sections with differentiated guidance, one line per mutant (position, change, `fp`, at most `maxLLMCoveringTests` short test names), the `operatorHints` legend emitted once per operator class, and a closing `## Next iteration` block pointing at `--fail-fast --mutant=<ids>`. Never present zero-kill or identical-kill-set tests as deletion candidates: most zero-kill tests cover code with no mutable operators. The shape is locked in by `testdata/golden.md` — regenerate with `GOLDEN_UPDATE=1`. `WriteSARIF` emits SARIF 2.1.0 (survived + not-covered mutants as results, package errors as notifications). The JSON carries `schema_version` (`report.SchemaVersion`; bump it when a field is removed or changes meaning), `summary.score_with_uncovered`, `summary.ignored`, `excluded_tests`, `stale_ignores`, `fail_fast`, a per-mutant `mutation.fingerprint`, and a per-mutant `note` when a re-run changed the verdict. Exposes a top-level `Errors` field (`omitempty`, `[]PackageError{package, stage, error}`) listing packages that were matched but not tested, rendered as "Packages not tested" in text and llm output, and a top-level `Scope` field (`omitempty`) describing the active filters; text output prefixes a `Scope:` header line. See `internal/report/report.go:33`.
- **`internal/corpus`** — downloads pinned real-world modules and requires every package's schema (all operators) to compile and pass its baseline. Runs only with `KANLY_CORPUS=1`; known baseline failures are listed with a reason, and one that starts passing fails the test. Add a module here when you find a construct the fixtures missed.
- **`internal/e2e`** — integration test that builds the CLI binary and runs it against `internal/runner/testdata/sample`, validating the JSON output against a pinned ledger. Read the report from stdout only (`stdoutOf`): stderr carries progress. See `internal/e2e/e2e_test.go:25`.

## Build & test

```bash
# Build the CLI
go build ./cmd/kanly

# Run everything (slow — includes the e2e binary build)
go test ./...

# Unit tests only (skips e2e and other slow tests)
go test -short ./...

# Real-world corpus: schema must compile + pass baseline (downloads modules)
KANLY_CORPUS=1 go test -timeout 30m ./internal/corpus/

# Regenerate the JSON golden file for internal/report
GOLDEN_UPDATE=1 go test ./internal/report
```

CI (`.github/workflows/ci.yml`) runs gofmt (excluding `testdata/`), `go vet`, staticcheck, `go test -race ./...`, and the corpus on every PR; the tests and corpus run on both the go.mod Go version and the latest stable release.

## Conventions

- **Table-driven tests** using the stdlib `testing` package only — no testify or other assertion libraries.
- **`relDir` helper** converts a path relative to the test file's source location to an absolute path. Used in `cmd/kanly/main_test.go:12`, `internal/e2e/e2e_test.go:12`, and `internal/report/report_test.go:17`.
- **Golden JSON files** live under `testdata/` next to the test file that owns them (e.g. `internal/report/testdata/golden.json`). Regenerate with `GOLDEN_UPDATE=1`.
- **Commit prefix:** `[task N]` where N is the 1-based line index of the task in `.claude/plan.md` (counting all task lines across the whole file). When no plan file exists, use `task: `.

## Pitfalls

### Use Underlying + Identical for symmetric integer/boolean operands

Integer and boolean operators accept any type whose underlying type is the
relevant basic — `int`, sized variants, `byte`, `rune`, named wrappers like
`type UserID int64`, and so on. The check pattern is:

```go
// CORRECT — IsInteger covers signed/unsigned, named, and sized variants.
b, ok := lv.Type.Underlying().(*types.Basic)
if !ok || b.Info()&types.IsInteger == 0 { return false }

// For binary operands, also require symmetry so `int + int32` isn't mutated:
if !types.Identical(lv.Type, rv.Type) { return false }
```

`intOperand` / `intOperands` / `boolOperand` / `boolOperands` in
`internal/operators/typecheck.go` encapsulate this. The symmetry guard is
redundant for non-constant binary operands (Go's type-checker enforces it
already) but is kept as defensive backstop.

The literal operators `int_literal`, `bool_literal`, and `string_literal`
are exceptions — they keep the strict-identity check
(`lv.Type == types.Typ[types.Int]`) because the rewriter cannot infer the
contextual type at an untyped-literal call site. Sized/named literal coverage is tracked in
`README.md`'s "Upcoming operators" section.

### A rewrite that doesn't compile aborts the whole package

Every dispatcher returns a concrete or inferred type, so check that it fits
the candidate's *contextual* type (`info.Types[expr].Type`, which records
what an untyped expression was converted to). Never emit a type name at the
call site — local, aliased, and same-package types can't be spelled
reliably; let generic inference do it (see `__cMutBool`). Skip positions Go
requires to be constant via `inConstantPosition`. Add any new hazard to
`internal/runner/testdata/tricky`, which `TestSchemaCompilesTrickyConstructs`
rewrites with every operator and compiles. Toolchains differ here (a
`//go:build go1.6` line sets that file's language version, and go1.23.0
enforces it), so run the suite on the go.mod Go version too, as CI does.

### Rewrite modifies the package AST in place

`schema.Rewrite` applies `astutil.Apply` to the loaded package's syntax
trees, so a second rewrite of the same `*source.Package` finds the
dispatcher calls instead of the original operators and produces nothing.
Load the package again for every rewrite (tests included).
