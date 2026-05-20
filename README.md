# Kanly

A Go mutation tester that uses Mutant Schema Generation to find tests that don't actually verify behaviour.

> **Status: MVP** — see [Supported operators](#supported-operators) and [Limitations](#limitations).

## Install

```
go install github.com/devenjarvis/kanly/cmd/kanly@latest
```

## Usage

```
kanly [--version] [--format=text|json|llm] [--timeout=30s] [--diff [--diff-base=<ref>]] [--tests=<regex>] [--mutant=<ids>] [--jobs=N] <pattern>[:<func-list>]...
```

| Flag | Default | Description |
|------|---------|-------------|
| `--version` | off | Print version and exit |
| `--format` | `text` | Output format: `text`, `json`, or `llm` (Markdown artifact tailored for feeding to an LLM) |
| `--timeout` | `30s` | Per-mutant test timeout |
| `--diff` | off | Only mutate lines changed since `--diff-base` |
| `--diff-base` | `HEAD` | Git ref to diff against when `--diff` is set |
| `--tests` | (all) | Regex narrowing the test inventory used by baseline and per-test coverage |
| `--mutant` | (all) | Comma-separated schema-assigned mutant IDs to re-run; others are skipped |
| `--jobs` | `NumCPU` | Parallel worker processes for per-test coverage and the mutant loop (`1` = sequential) |

Positional arguments take the form `<pkg-pattern>[:<func-list>]`. The optional function list selects mutants by enclosing function — accepts plain names (`Foo`), method receivers in dotted form (`Server.Handle` matches both `(Server).Handle` and `(*Server).Handle`), or the explicit canonical form (`(*Server).Handle`). A function filter requires the pattern to resolve to a single package (no `./...`).

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

Each package's pipeline (rewrite → compile → baseline → mutant loop) runs independently. Packages with no test files or no mutations are skipped with a one-line stderr notice and the run continues. Within a package the per-test coverage pass and the mutant loop both run concurrently under a worker pool sized by `--jobs` (default `runtime.NumCPU()`; pass `--jobs=1` for serial execution). Cross-package parallelism is still sequential and is a planned future addition.

### Focused mutation

For tight LLM iteration loops, narrow the scope so the schema, test selection, and report stay sharp:

```
# Mutate one function; the report's test inventory drops tests that don't cover Add.
$ kanly ./internal/runner/testdata/sample:Add

# Multiple functions in one package, including a method.
$ kanly './internal/foo:Compute,(*Server).Handle'

# Pre-narrow the test inventory (skips baseline + coverage for everything else).
$ kanly --tests='^TestCompute' ./internal/foo:Compute

# Re-verify two survivors after editing the test suite.
$ kanly --mutant=7,12 ./internal/foo

# Compose with --diff: only changed lines inside the named function.
$ kanly --diff ./internal/foo:Compute
```

When a scope is active (`pkg:Func`, `--tests`, `--mutant`, or `--diff`), the report's `scope` field and the text-mode `Scope:` header announce exactly what was tested. The inventory used for `zero_kill_tests` / `redundant_test_groups` is also narrowed to tests that touch a kept mutation, so the signal stays focused on the targeted change.

## How it works

Kanly implements the Mutant Schema Generation (MSG) technique: instead of recompiling once per mutant, it encodes all mutants into a single metaprogram, compiles once, and selects active mutants at runtime via the `KANLY_MUTANT` environment variable.

## Supported operators

| Operator | Mutations | Research basis |
|---|---|---|
| `int_arith` | `+↔-`, `*↔/`, `%→*` | AOR (Arithmetic Operator Replacement); PIT MATH mutator |
| `int_cmp_boundary` | `<↔<=`, `>↔>=` | PIT CONDITIONALS\_BOUNDARY mutator |
| `int_cmp_negate` | `<→>=`, `>→<=`, `<=→>`, `>=→<`, `==→!=`, `!=→==` | PIT NEGATE\_CONDITIONALS mutator |
| `int_bitwise` | `&↔\|`, `^→&`, `<<↔>>`, `&^→&` | PIT MATH (bitwise variant) |
| `int_literal` | `n→0`, `n→1`, `n→-n`, `n→n±1` (excluding no-ops) | LVR (Literal Value Replacement) |
| `bool_logic` | `&&↔\|\|` | PIT CONDITIONALS\_NEGATION (logical) |
| `bool_not` | `!→⌀` | PIT NEGATE\_CONDITIONALS (unary) |
| `bool_literal` | `true↔false` (predeclared keyword, contextual type exactly `bool`) | LCR (Logical Constant Replacement) |
| `err_return_nil` | `err→nil` (any `error`-typed expr in a `return` stmt) | PIT EMPTY\_RETURNS family |
| `return_zero` | `return x → return zero(T)` (`int`→`0`, `string`→`""`, `bool`→`false`, ptr/slice/map/chan/func/iface→`nil`) | PIT EMPTY\_RETURNS family |
| `call_delete` | `f(...)→⌀` (any void-discarded call statement) | PIT VOID\_METHOD\_CALLS mutator |
| `string_literal` | non-empty `"x"`→`""` (and raw backtick strings) | PIT EMPTY\_RETURNS / LVR (string variant) |
| `slice_index` | `a[i]`→`a[i+1]`, `a[i]`→`a[i-1]` | PIT INCREMENTS (index-position variant) |
| `slice_range` | `s[lo:hi]`→`s[lo±1:hi]`, `s[lo:hi±1]` (and three-index `s[lo:hi:max]` bounds) | PIT INCREMENTS (range-position variant) |
| `inc_dec` | `x++↔x--` (statement form) | PIT INCREMENTS mutator |
| `int_compound_assign` | `+=↔-=`, `*=↔/=`, `%=→*=`, `&=↔\|=`, `^=→&=`, `<<=↔>>=`, `&^=→&=` | AOR (statement-level), PIT MATH (compound variant) |
| `struct_field_zero` | keyed struct-literal field value → zero of T (shares the `return_zero` dispatcher) | PIT EMPTY\_RETURNS family (field-position variant) |

Integer operators (`int_arith`, `int_cmp_*`, `int_bitwise`, `slice_index`, `slice_range`, `inc_dec`, `int_compound_assign`) target operands whose underlying type is a basic integer — `int`, sized signed/unsigned variants (`int8`/`int32`/`uint64`/…), `byte`, `rune`, `uintptr`, and named wrappers like `type UserID int64` or `time.Duration`. Binary forms additionally require both operands to share the same defined type (`types.Identical`), so `int + int32` is not mutated cross-type. Boolean operators (`bool_logic`, `bool_not`) follow the same policy for `bool` and named-bool wrappers like `type MyBool bool`. Type-bearing dispatchers are generic (`func __cMutInt[T __cInteger](a, b T, …) T`) so the rewritten code stays type-safe across the full family. `int_literal` and `bool_literal` are exceptions — they remain restricted to plain `int` / `bool` because the rewriter cannot infer the contextual type at an untyped-literal call site; sized/named literal coverage is tracked in [Upcoming operators](#upcoming-operators). `err_return_nil` restricts to expressions whose static type is exactly the universe `error` interface (not concrete types like `*MyError`), and ignores naked `return` statements in functions with named results. `call_delete` targets `*ast.ExprStmt`-wrapped calls only (so `defer` and `go` statements are naturally excluded); it skips the builtins `panic`, `print`, and `println` but keeps `close`, `delete`, `clear`, and `recover`, whose deletion produces observable behavior changes (hangs, residual map entries, propagated panics). Under an active mutant the call's argument expressions are not evaluated either — lazy semantics matching PIT's "Void Method Call Removal". `string_literal` skips empty strings, import paths, struct field tags, and const-decl initializers (each must syntactically remain a literal in Go and cannot be replaced by a function call). `slice_index` and `slice_range` exclude purely-constant indices. `inc_dec` and `int_compound_assign` use a shared closure-swap dispatcher so non-trivial LHS forms like `m[k]++` or `m[k] += y` are evaluated exactly once.

## Upcoming operators

These mutators are planned but not yet implemented. Their absence is the
largest remaining gap in Kanly's coverage:

- **`float_arith` + `float_cmp_*`** — `+ - * /` and the six comparisons on float operands (`float32`, `float64`, named wrappers). Direct float counterparts to the integer operators; will reuse the underlying-`IsFloat` predicate pattern introduced for integers.
- **`chan_op`** — channel-receive removal in expression position (`<-ch` → zero value of the element type) and channel-send statement deletion (`ch <- x` → no-op). `delete(m, k)` and `close(ch)` already drop out as call-statement removals via `call_delete`; this operator covers the missing receive/send corners.
- **`switch_case`** — drop a case clause (fall-through to the next clause or default) and swap adjacent case bodies. Type-switches and value-switches need distinct handling; the operator must preserve `case` headers to avoid compile errors.
- **Sized/named integer & boolean literal mutation** — extending `int_literal` and `bool_literal` to fire on contexts like `var x int32 = 5` or `var b MyBool = true`. The contextual type isn't inferable from an untyped operand alone, so this requires emitting explicit type arguments or conversions at the call site.

## Operators considered and rejected

- **Function-call argument swapping (same-typed adjacent args)** — a classic mutation operator (swap `f(a, b)` to `f(b, a)` for same-typed parameters) but with a notoriously high false-positive rate. Many same-typed argument pairs are semantically commutative (`Add(a, b int)`, set-membership checks, comparison helpers), producing equivalent or near-equivalent mutants. Modern mutation testers (PIT, mutmut, Stryker) have all de-emphasized or removed this operator for the same reason; Kanly will not implement it.

## Limitations

- **Narrow operator surface.** Today Kanly mutates arithmetic operators (expression and statement forms — `+ - * / %` and `+= -= *= /= %=`), integer comparisons, integer bitwise/shift operators (binary and compound), boolean logic, integer increment/decrement, integer-literal values, `error`-typed return values, full return-value zeroing for integer/string/bool/nilable types, void-discarded call statements, non-empty string literals, slice/map indices, and slice-range bounds. Float arithmetic and channel/switch operators are not yet covered — see [Upcoming operators](#upcoming-operators). Deleting side-effecting calls (e.g. `close(ch)`, locks, or sync primitives) can cause hangs in concurrent code, and inverting a loop counter (`i++→i--`) likewise loops forever; the per-mutant `--timeout` bounds these and surfaces them as `Timeout` rather than `Survived`.
- **Test-centric signals depend on operator coverage.** The `tests`, `zero_kill_tests`, and `survivors_by_function` views in the JSON report are only as sharp as the underlying operator set. Today, many entries in `zero_kill_tests` are tests exercising code that has no mutable operators in it — not tests with weak assertions. Read these views as exploration aids; broadening the operator set will turn them into a reliable test-quality signal.
- **Some assertions are structurally unkillable.** A few test patterns can't be expressed as MSG mutants no matter how many operators are added — most notably tests that assert on struct field tags (e.g., JSON tag names read via reflection), because Go syntax requires struct tags to be uninterpreted string literals that cannot be rewritten to runtime-conditional expressions. Such tests will always appear in `zero_kill_tests`; treat them as "structurally unkillable" rather than weak.
- **"Identical kill-sets" is not the same as "redundant tests."** Two tests can land in the same `redundant_test_groups` entry because they coincidentally trip the same mutated lines while asserting on entirely different concerns. Treat the grouping as a "look here" prompt, not a "delete one" recommendation.
- **Sequential multi-package only.** `./...` and multiple positional args are supported; packages run sequentially. Within a package the per-test coverage pass and the mutant loop are already parallel (`--jobs N`, default `runtime.NumCPU()`); cross-package parallelism is tracked for a future release.

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

## LLM-driven test repair

`--format=llm` emits a Markdown artifact designed to feed into an LLM that will write new tests for survivors and tighten weak assertions. Its layout is informed by Wang, Xu, Briand & Liu (2025): live (covered-but-survived) mutants and uncovered mutants get separate sections with different prompt-side guidance — sharpen an assertion vs. add a test that reaches the line — because they need different fixes. Each operator class drops in a one-line strategy hint on its first occurrence (boundary values for `int_cmp_boundary`, sign-changing inputs for `int_arith`, exact-string assertions for `string_literal`, and so on). The artifact closes with a `kanly --mutant=<ids>` command that re-runs only the survivor schema IDs — the iterative verify-after-edit loop validated in the same paper.

## References

- DeMillo, R. A., Lipton, R. J., & Sayward, F. G. (1978). Hints on test data selection: Help for the practicing programmer. *IEEE Computer*, 11(4), 34–41.
- Offutt, A. J., & Untch, R. H. (2001). Mutation 2000: Uniting the orthogonal. In *Mutation Testing for the New Century* (pp. 34–44). Springer.
- Just, R., Jalali, D., Inozemtseva, L., Ernst, M. D., Holmes, R., & Fraser, G. (2014). Are mutants a valid substitute for real faults in software testing? In *Proceedings of FSE 2014* (pp. 654–665). ACM.
- Coles, H., Laurent, T., Henard, C., Papadakis, M., & Ventresque, A. (2016). PIT: A practical mutation testing tool for Java. In *Proceedings of ISSTA 2016* (pp. 449–452). ACM.
- Wang, G., Xu, Q., Briand, L., & Liu, K. (2025). Mutation-Guided Unit Test Generation with a Large Language Model. *arXiv preprint* [arXiv:2506.02954](https://arxiv.org/abs/2506.02954). Basis for the `--format=llm` artifact structure: live-vs-uncovered split, operator-class hints, and the `--mutant=<ids>` iterate-after-edit loop.
- PIT mutator reference: https://pitest.org/quickstart/mutators/
