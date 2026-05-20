# Kanly mutation report

**Scope:** `./...`

## Task

Your goal is to **raise the mutation score** of this package: write tests that fail when the listed mutations are applied. Line coverage is not the target — a test suite can reach 100% coverage and still kill almost no mutants. Two kinds of offense target follow:

- **Live mutants** are reached by existing tests but not detected — the fix is to *sharpen an assertion* so the original and mutant diverge.
- **Uncovered mutants** are never executed — the fix is to *add a test* that exercises the line; almost any assertion on its result will catch the mutation.

After editing tests, verify with `kanly --mutant=<ids> <pkg>` (see the iteration block at the end of this report) to re-run only the targeted mutants.

## Summary

| Total | Killed | Survived | Timeout | NotCovered | NotViable | Score |
|-------|--------|----------|---------|------------|-----------|-------|
| 3 | 1 | 1 | 0 | 1 | 0 | 50.0% |

## Live mutants (covered but not killed)

_Existing tests reach the mutated line but don't observe the change. Sharpen an assertion so the original and mutant diverge._

### example.com/pkg/foo — Add  (1)

**Source: foo.go:3-6**

```go
3  func Add(a, b int) int {
4  	// no-op comment
5  	return a + b + 1
6  }
```

- **#2** at foo.go:5:14 — `int_literal` `1` → `0`
  - _Hint (`int_literal`): Assert the exact numeric result so swapping the literal to 0/1/±1 fails._
  - Covering tests that did NOT kill: `example.com/pkg/foo.TestAdd`, `example.com/pkg/foo.TestAddSmoke`

## Uncovered mutants (no test reaches this line)

_No test executes this code. Add a new test that drives the path; almost any meaningful assertion on the result will catch the mutation._

### example.com/pkg/bar — Mul  (1)

**Source: bar.go:3-3**

```go
3  func Mul(a, b int) int { return a * b }
```

- **#3** at bar.go:3:15 — `int_arith` `*` → `/`
  - _Hint (`int_arith`): Use inputs where +/-/*/% differ; avoid zero/identity values that mask the operator._
  - Covering tests that did NOT kill: _none — mutation site is not exercised by any test_

## Redundant test groups (consolidation targets)

_None — no two tests share an identical kill set._

## Zero-kill tests (deletion candidates)

These tests killed no mutants within the current scope. Consider deleting or rewriting them to target a surviving mutant above.

- `example.com/pkg/bar.TestMul`
- `example.com/pkg/foo.TestAddSmoke`

## Test inventory

| Test | KillCount | Killed mutants |
|------|-----------|----------------|
| `example.com/pkg/foo.TestAdd` | 1 | #1 |
| `example.com/pkg/bar.TestMul` | 0 | _none_ |
| `example.com/pkg/foo.TestAddSmoke` | 0 | _none_ |

## Next iteration

After editing tests, re-run only the targeted mutants to verify each is now killed — this skips the compile + baseline + per-test coverage passes for unaffected mutants and is the iterative loop validated in arXiv:2506.02954:

```
kanly --mutant=2,3 <pkg>
```

Any survivor still listed after the re-run is a real assertion gap — feed it back through this artifact and iterate.

