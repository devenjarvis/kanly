# Cauldron

A Go mutation tester that uses Mutant Schema Generation to find tests that don't actually verify behaviour.

> **Status: MVP** — integer arithmetic mutations only; see [Limitations](#limitations).

## Install

```
go install github.com/devenjarvis/cauldron/cmd/cauldron@latest
```

## Usage

```
cauldron [--format=text|json] [--timeout=30s] <package-dir>
```

| Flag | Default | Description |
|------|---------|-------------|
| `--format` | `text` | Output format: `text` or `json` |
| `--timeout` | `30s` | Per-mutant test timeout |

### Worked example

```
$ cauldron ./internal/runner/testdata/sample
internal/runner/testdata/sample/sample.go:5:35 [int_arith] -→+

Total: 2 | Killed: 1 | Survived: 1 | Timeout: 0 | Score: 50.0%
```

The survived mutant on line 5 (`Sub`) means the test suite has a weak assertion on subtraction — it passes even when `-` is replaced with `+`.

## How it works

Cauldron implements the Mutant Schema Generation (MSG) technique: instead of recompiling once per mutant, it encodes all mutants into a single metaprogram, compiles once, and selects active mutants at runtime via the `CAULDRON_MUTANT` environment variable.

## Limitations

- **Integer arithmetic only.** The current operator mutates `+`, `-`, `*`, and `/` on operands whose type is exactly `int`. Named int types (`type MyInt int`) and sized types (`int32`, `int64`) are not mutated. See `internal/operators/arithmetic.go`.
- **One package at a time.** Pass a single `<package-dir>` argument; there is no built-in `./...` expansion yet.

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

Every pull request on this repo runs Cauldron against `./internal/report` via the [mutation-test](.github/workflows/mutation-test.yml) GitHub Actions workflow. The workflow posts the result as a single PR comment (updated in-place on re-pushes) and never fails the check — it is informational only.

**Fork PRs:** `GITHUB_TOKEN` on fork pull requests does not receive `pull-requests: write` permission, so the comment step will silently no-op for contributions from forks.
