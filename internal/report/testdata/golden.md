# Kanly mutation report

**Scope:** `./...`

## Task

Raise the **mutation score**: each mutant below is a small change to the code that no test noticed. Line coverage is not the target. Decide each mutant one of three ways:

1. **Test it.** For a live mutant (tests run the line but none failed), sharpen an assertion so the original and the mutant diverge. For an uncovered mutant, add a test that reaches the line.
2. **Simplify the code.** A mutant nothing can observe may reveal dead or redundant code, such as a field that is never read or a check repeated later. Delete it and the mutant goes with it.
3. **Accept it.** If the mutant is equivalent (no input tells it apart from the original) or only a contrived test could kill it, append `<fp> <reason>` to `.kanlyignore` in the package's directory. Kanly stops reporting it until its line changes.

Write tests a maintainer would keep: assert on behaviour callers can observe through the package's API. Match errors with `errors.Is`/`errors.As` or a key substring, not the full message, and don't assert on unexported fields, log text or call order just to kill a mutant. A test that breaks on a harmless refactor costs more than a surviving mutant; accept the mutant instead.

## Summary

| Total | Killed | Survived | Timeout | NotCovered | NotViable | Score | Score incl. not covered |
|-------|--------|----------|---------|------------|-----------|-------|-------------------------|
| 3 | 1 | 1 | 0 | 1 | 0 | 50.0% | 33.3% |

## Live mutants (covered but not killed)

_Tests run these lines, but none failed when the mutant was applied. Sharpen an assertion, simplify the code, or accept the mutant._

### example.com/pkg/foo — Add  (1)

**Source: foo.go:3-6**

```go
3  func Add(a, b int) int {
4  	// no-op comment
5  	return a + b + 1
6  }
```

- **#2** L5:14 `int_literal` `1` → `0` · fp `5d41402abc4b` · ran by `TestAdd`, `TestAddSmoke`
  - _Hint (`int_literal`): Assert the exact numeric result so swapping the literal to 0/1/±1 fails. A size or capacity that only affects performance is usually not worth a test._

## Uncovered mutants (no test reaches this line)

_No test in the package runs these lines. Add a test that drives the path; almost any meaningful assertion on the result will catch the mutation. Code exercised only by another package's tests shows up here too: re-run with `--cross-package` to count those tests before writing new ones._

### example.com/pkg/bar — Mul  (1)

**Source: bar.go:3-3**

```go
3  func Mul(a, b int) int { return a * b }
```

- **#3** L3:15 `int_arith` `*` → `/` · fp `7d793037a076`
  - _Hint (`int_arith`): Use inputs where +/-/*/% differ; avoid zero/identity values that mask the operator._

## Tests with identical kill sets

_None — no two tests share an identical kill set._

## Tests that killed no mutants

**Do not delete these tests on this basis.** Most exercise code with no mutable operators (constructors, wiring, struct tags) or check behaviour Kanly's operators don't change. A test that ran a live mutant above may have a weak assertion; those are marked.

- `example.com/pkg/bar.TestMul`
- `example.com/pkg/foo.TestAddSmoke` — ran a live mutant

## Test inventory

Mutants killed per test:

- `example.com/pkg/bar`: `TestMul` 0
- `example.com/pkg/foo`: `TestAdd` 1, `TestAddSmoke` 0

## Next iteration

After editing tests or code, re-run only these mutants (the iterative loop validated in arXiv:2506.02954):

```
kanly --mutant=7d793037a076 example.com/pkg/bar
kanly --mutant=5d41402abc4b example.com/pkg/foo
```

Mutants are named by fingerprint (`fp`), which survives edits elsewhere in the package. Editing a mutant's own line gives it a new fingerprint, and the re-run reports the old one as not found: re-run the package or `<pkg>:<Func>` to judge that line again.

To try one test against one mutant while you write it, run `kanly test <pkg> <fp> -run <TestName>`: it runs `go test` with the mutant applied, so the test should fail. Add `--in=<pkg>` for a test in another package. `kanly show <pkg> <fp>` prints the mutated line.

