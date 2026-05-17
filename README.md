# Kanly

A Go mutation tester that uses Mutant Schema Generation to find tests that don't actually verify behaviour.

> **Status: MVP** — see [Supported operators](#supported-operators) and [Limitations](#limitations).

## Install

```
go install github.com/devenjarvis/kanly/cmd/kanly@latest
```

## Usage

```
kanly [--format=text|json] [--timeout=30s] <pattern>...
```

| Flag | Default | Description |
|------|---------|-------------|
| `--format` | `text` | Output format: `text` or `json` |
| `--timeout` | `30s` | Per-mutant test timeout |

### Worked example

```
$ kanly ./internal/runner/testdata/sample
internal/runner/testdata/sample/sample.go:5:35 [int_arith] -→+
Package: github.com/devenjarvis/kanly/internal/runner/testdata/sample | Total: 2 | Killed: 1 | Survived: 1 | Score: 50.0%

Total: 2 | Killed: 1 | Survived: 1 | Timeout: 0 | Score: 50.0%
```

The survived mutant on line 5 (`Sub`) means the test suite has a weak assertion on subtraction — it passes even when `-` is replaced with `+`.

### Multi-package

Pass multiple patterns or use `./...` to mutate all packages in a module in a single run:

```
$ kanly ./internal/runner/testdata/sample ./internal/runner/testdata/cmpsample
internal/runner/testdata/sample/sample.go:5:35 [int_arith] -→+
Package: github.com/devenjarvis/kanly/internal/runner/testdata/cmpsample | Total: 2 | Killed: 2 | Survived: 0 | Score: 100.0%
Package: github.com/devenjarvis/kanly/internal/runner/testdata/sample | Total: 2 | Killed: 1 | Survived: 1 | Score: 50.0%

Total: 4 | Killed: 3 | Survived: 1 | Timeout: 0 | Score: 75.0%
```

Each package's pipeline (rewrite → compile → baseline → mutant loop) runs independently. Packages with no test files or no mutations are skipped with a one-line stderr notice and the run continues. Package-level parallelism is a planned future addition.

## How it works

Kanly implements the Mutant Schema Generation (MSG) technique: instead of recompiling once per mutant, it encodes all mutants into a single metaprogram, compiles once, and selects active mutants at runtime via the `KANLY_MUTANT` environment variable.

## Supported operators

| Operator | Mutations | Research basis |
|---|---|---|
| `int_arith` | `+↔-`, `*↔/`, `%→*` | AOR (Arithmetic Operator Replacement); PIT MATH mutator |
| `int_cmp_boundary` | `<↔<=`, `>↔>=` | PIT CONDITIONALS\_BOUNDARY mutator |
| `int_cmp_negate` | `<→>=`, `>→<=`, `<=→>`, `>=→<`, `==→!=`, `!=→==` | PIT NEGATE\_CONDITIONALS mutator |
| `bool_logic` | `&&↔\|\|` | PIT CONDITIONALS\_NEGATION (logical) |
| `bool_not` | `!→⌀` | PIT NEGATE\_CONDITIONALS (unary) |
| `err_return_nil` | `err→nil` (any `error`-typed expr in a `return` stmt) | PIT EMPTY\_RETURNS family |
| `call_delete` | `f(...)→⌀` (any void-discarded call statement) | PIT VOID\_METHOD\_CALLS mutator |
| `string_literal` | non-empty `"x"`→`""` (and raw backtick strings) | PIT EMPTY\_RETURNS / LVR (string variant) |
| `slice_index` | `a[i]`→`a[i+1]`, `a[i]`→`a[i-1]` (index type exactly `int`) | PIT INCREMENTS (index-position variant) |

Arithmetic and comparison operators restrict to operands whose type is exactly `int` (not `int32`, `int64`, or named types like `type MyInt int`); boolean operators restrict to operands whose type is exactly `bool` (not named types like `type MyBool bool`). `err_return_nil` restricts to expressions whose static type is exactly the universe `error` interface (not concrete types like `*MyError`), and ignores naked `return` statements in functions with named results. `call_delete` targets `*ast.ExprStmt`-wrapped calls only (so `defer` and `go` statements are naturally excluded); it skips the builtins `panic`, `print`, and `println` but keeps `close`, `delete`, `clear`, and `recover`, whose deletion produces observable behavior changes (hangs, residual map entries, propagated panics). Under an active mutant the call's argument expressions are not evaluated either — lazy semantics matching PIT's "Void Method Call Removal". `string_literal` skips empty strings, import paths, struct field tags, and const-decl initializers (each must syntactically remain a literal in Go and cannot be replaced by a function call). `slice_index` requires the index expression to have static type exactly `int` and excludes purely-constant indices, mirroring the strict-identity policy used by the arithmetic operators.

## Limitations

- **Plain `int` only.** Named int types (`type MyInt int`) and sized types (`int32`, `int64`) are not mutated. See `internal/operators/typecheck.go`.
- **Narrow operator surface.** Today Kanly mutates arithmetic operators, integer comparisons, boolean logic, `error`-typed return values, void-discarded call statements, non-empty string literals, and int-typed slice/map indices. Code dominated by integer-literal constants (e.g. magic-number returns) or `nil`-receiver checks still emits few or no mutants. Remaining high-value families: integer-literal mutation, return-value tweaks (zero / one), and slice-range boundary mutation. Deleting side-effecting calls (e.g. `close(ch)`, locks, or sync primitives) can cause hangs in concurrent code; the per-mutant `--timeout` bounds this and surfaces them as `Timeout` rather than `Survived`.
- **Test-centric signals depend on operator coverage.** The `tests`, `zero_kill_tests`, and `survivors_by_function` views in the JSON report are only as sharp as the underlying operator set. Today, many entries in `zero_kill_tests` are tests exercising code that has no mutable operators in it — not tests with weak assertions. Read these views as exploration aids; broadening the operator set will turn them into a reliable test-quality signal.
- **"Identical kill-sets" is not the same as "redundant tests."** Two tests can land in the same `redundant_test_groups` entry because they coincidentally trip the same mutated lines while asserting on entirely different concerns. Treat the grouping as a "look here" prompt, not a "delete one" recommendation.
- **Sequential multi-package only.** `./...` and multiple positional args are supported; packages run sequentially. Package-level parallelism is tracked for a future release.

## Development

```bash
# Run all tests (including the slow e2e build+run test)
go test ./...

# Run unit tests only (skips e2e)
go test -short ./...

# Regenerate JSON golden files
GOLDEN_UPDATE=1 go test ./internal/report
```

## Dogfooding

Every pull request on this repo runs Kanly against `./internal/report ./internal/mutation` via the [mutation-test](.github/workflows/mutation-test.yml) GitHub Actions workflow. The workflow posts the result as a single PR comment (updated in-place on re-pushes) and never fails the check — it is informational only.

**Fork PRs:** `GITHUB_TOKEN` on fork pull requests does not receive `pull-requests: write` permission, so the comment step will silently no-op for contributions from forks.

## References

- DeMillo, R. A., Lipton, R. J., & Sayward, F. G. (1978). Hints on test data selection: Help for the practicing programmer. *IEEE Computer*, 11(4), 34–41.
- Offutt, A. J., & Untch, R. H. (2001). Mutation 2000: Uniting the orthogonal. In *Mutation Testing for the New Century* (pp. 34–44). Springer.
- Just, R., Jalali, D., Inozemtseva, L., Ernst, M. D., Holmes, R., & Fraser, G. (2014). Are mutants a valid substitute for real faults in software testing? In *Proceedings of FSE 2014* (pp. 654–665). ACM.
- Coles, H., Laurent, T., Henard, C., Papadakis, M., & Ventresque, A. (2016). PIT: A practical mutation testing tool for Java. In *Proceedings of ISSTA 2016* (pp. 449–452). ACM.
- PIT mutator reference: https://pitest.org/quickstart/mutators/
