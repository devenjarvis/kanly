# Cauldron

A Go mutation tester that uses Mutant Schema Generation to find tests that don't actually verify behaviour.

> **Status: MVP** — see [Supported operators](#supported-operators) and [Limitations](#limitations).

## Install

```
go install github.com/devenjarvis/cauldron/cmd/cauldron@latest
```

## Usage

```
cauldron [--format=text|json] [--timeout=30s] <pattern>...
```

| Flag | Default | Description |
|------|---------|-------------|
| `--format` | `text` | Output format: `text` or `json` |
| `--timeout` | `30s` | Per-mutant test timeout |

### Worked example

```
$ cauldron ./internal/runner/testdata/sample
internal/runner/testdata/sample/sample.go:5:35 [int_arith] -→+
Package: github.com/devenjarvis/cauldron/internal/runner/testdata/sample | Total: 2 | Killed: 1 | Survived: 1 | Score: 50.0%

Total: 2 | Killed: 1 | Survived: 1 | Timeout: 0 | Score: 50.0%
```

The survived mutant on line 5 (`Sub`) means the test suite has a weak assertion on subtraction — it passes even when `-` is replaced with `+`.

### Multi-package

Pass multiple patterns or use `./...` to mutate all packages in a module in a single run:

```
$ cauldron ./internal/runner/testdata/sample ./internal/runner/testdata/cmpsample
internal/runner/testdata/sample/sample.go:5:35 [int_arith] -→+
Package: github.com/devenjarvis/cauldron/internal/runner/testdata/cmpsample | Total: 2 | Killed: 2 | Survived: 0 | Score: 100.0%
Package: github.com/devenjarvis/cauldron/internal/runner/testdata/sample | Total: 2 | Killed: 1 | Survived: 1 | Score: 50.0%

Total: 4 | Killed: 3 | Survived: 1 | Timeout: 0 | Score: 75.0%
```

Each package's pipeline (rewrite → compile → baseline → mutant loop) runs independently. Packages with no test files or no mutations are skipped with a one-line stderr notice and the run continues. Package-level parallelism is a planned future addition.

## How it works

Cauldron implements the Mutant Schema Generation (MSG) technique: instead of recompiling once per mutant, it encodes all mutants into a single metaprogram, compiles once, and selects active mutants at runtime via the `CAULDRON_MUTANT` environment variable.

## Supported operators

| Operator | Mutations | Research basis |
|---|---|---|
| `int_arith` | `+↔-`, `*↔/`, `%→*` | AOR (Arithmetic Operator Replacement); PIT MATH mutator |
| `int_cmp_boundary` | `<↔<=`, `>↔>=` | PIT CONDITIONALS\_BOUNDARY mutator |
| `int_cmp_negate` | `<→>=`, `>→<=`, `<=→>`, `>=→<`, `==→!=`, `!=→==` | PIT NEGATE\_CONDITIONALS mutator |
| `bool_logic` | `&&↔\|\|` | PIT CONDITIONALS\_NEGATION (logical) |
| `bool_not` | `!→⌀` | PIT NEGATE\_CONDITIONALS (unary) |

Arithmetic and comparison operators restrict to operands whose type is exactly `int` (not `int32`, `int64`, or named types like `type MyInt int`); boolean operators restrict to operands whose type is exactly `bool` (not named types like `type MyBool bool`).

## Limitations

- **Plain `int` only.** Named int types (`type MyInt int`) and sized types (`int32`, `int64`) are not mutated. See `internal/operators/typecheck.go`.
- **No statement-level mutators.** Statement deletion and return-value replacement need a different operator shape; tracked for a future release.
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

Every pull request on this repo runs Cauldron against `./internal/report ./internal/mutation` via the [mutation-test](.github/workflows/mutation-test.yml) GitHub Actions workflow. The workflow posts the result as a single PR comment (updated in-place on re-pushes) and never fails the check — it is informational only.

**Fork PRs:** `GITHUB_TOKEN` on fork pull requests does not receive `pull-requests: write` permission, so the comment step will silently no-op for contributions from forks.

## References

- DeMillo, R. A., Lipton, R. J., & Sayward, F. G. (1978). Hints on test data selection: Help for the practicing programmer. *IEEE Computer*, 11(4), 34–41.
- Offutt, A. J., & Untch, R. H. (2001). Mutation 2000: Uniting the orthogonal. In *Mutation Testing for the New Century* (pp. 34–44). Springer.
- Just, R., Jalali, D., Inozemtseva, L., Ernst, M. D., Holmes, R., & Fraser, G. (2014). Are mutants a valid substitute for real faults in software testing? In *Proceedings of FSE 2014* (pp. 654–665). ACM.
- Coles, H., Laurent, T., Henard, C., Papadakis, M., & Ventresque, A. (2016). PIT: A practical mutation testing tool for Java. In *Proceedings of ISSTA 2016* (pp. 449–452). ACM.
- PIT mutator reference: https://pitest.org/quickstart/mutators/
