# Kanly

A Go mutation tester that uses Mutant Schema Generation to find tests that don't actually verify behaviour.

> **Status: MVP** — see [Supported operators](#supported-operators) and [Limitations](#limitations).

## Install

```
go install github.com/devenjarvis/kanly/cmd/kanly@latest
```

## Usage

```
kanly [flags] <pattern>[:<func-list>]...
```

| Flag | Default | Description |
|------|---------|-------------|
| `--version` | off | Print version and exit |
| `--format` | `text` | Output format: `text`, `json`, `llm` (Markdown artifact tailored for feeding to an LLM), or `sarif` (SARIF 2.1.0 for code-scanning annotations) |
| `--min-score` | `0` | Exit 3 when the score including not-covered mutants is below this percentage |
| `--timeout` | derived | Fixed per-mutant test timeout. By default each mutant's timeout is derived from the baseline: `--timeout-factor` × (the baseline run time of the tests it runs + process start-up) + 2s |
| `--timeout-factor` | `3` | Multiple of the baseline run time a mutant may take before it counts as `Timeout` (ignored when `--timeout` is set) |
| `--diff` | off | Only mutate lines changed since the working tree diverged from `--diff-base` (see [Using Kanly in CI](#using-kanly-in-ci)) |
| `--diff-base` | `HEAD` | Git ref to diff against when `--diff` is set |
| `--tests` | (all) | Regex narrowing the test inventory used by baseline and per-test coverage |
| `--mutant` | (all) | Comma-separated mutant IDs or [fingerprints](#accepting-mutants) to re-run; others are skipped. A fingerprint that matches no mutant (its line changed) is reported |
| `--operators` | (all) | Comma-separated operators to run |
| `--skip-operators` | (none) | Comma-separated operators not to run |
| `--exclude` | (none) | Comma-separated file globs not to mutate, matched against the path relative to the working directory, the file name, or a directory prefix (`internal/gen`, `*_mock.go`) |
| `--tags` | (none) | Build tags, as for `go build -tags`; tagged files are mutated and tagged tests run |
| `--short` | off | Run tests with `-test.short` |
| `--jobs` | `NumCPU` | Parallel worker processes for per-test coverage and the mutant loop (`1` = sequential) |
| `--verify-kills` | off | Re-run every killed mutant alone and count kills that do not reproduce as survived (roughly doubles the run time) |
| `--fail-fast` | off | Stop testing each mutant at its first failing test. Verdicts are unchanged, but each kill names one test, so the per-test views (`tests`, `zero_kill_tests`, `redundant_test_groups`) are left out. Meant for the inner loop of re-running a few mutants |
| `--cross-package` | off | Also run the tests of the module's other packages that import the mutated package, so code tested through another package (handler tests, integration suites) counts as covered. See [Cross-package tests](#cross-package-tests) |
| `--no-cache` | off | With `--cross-package`, set up every dependent's tests afresh instead of reusing an unchanged setup |
| `--llm-limit` | `30` | Most mutants the `--format=llm` brief lists, the functions with the most live mutants first; the functions left out are named. `0` lists all |
| `--quiet` | off | Print no progress to stderr; errors and warnings are still printed |

Positional arguments take the form `<pkg-pattern>[:<func-list>]`. The optional function list selects mutants by enclosing function — accepts plain names (`Foo`), method receivers in dotted form (`Server.Handle` matches both `(Server).Handle` and `(*Server).Handle`), or the explicit canonical form (`(*Server).Handle`). A function filter requires the pattern to resolve to a single package (no `./...`).

### Exit status, failed packages and excluded tests

A test that fails with no mutant applied cannot tell a mutant from the original code, so Kanly leaves it out instead of giving up on the package. Excluded tests are listed under `Tests excluded` in text and `--format=llm` output and in the JSON `excluded_tests` array (`package`, `test`, `reason`). The reason says which of three cases applies: the test fails on the unmodified code; it fails only under Kanly's rewritten code (tests that count allocations or inspect call stacks do this; anything else is a Kanly bug worth reporting); or it failed in the full run but passes alone, or fails when run on its own (flaky or order-dependent).

A package that cannot be mutation-tested at all — it doesn't load or type-check, its schema doesn't compile, or its test binary fails outside any single test (for example in `TestMain`) — is recorded and skipped; the other packages still run and are reported. Failed packages appear under `Packages not tested` in text and `--format=llm` output and in the JSON `errors` array (`package`, `stage`, `error`), where `stage` is one of `load`, `rewrite`, `compile`, `baseline`, `coverage`, `mutants`. A `baseline` error says whether the unmodified code fails too; a `compile` failure is most likely a Kanly bug worth reporting.

| Exit | Meaning |
|------|---------|
| `0` | Every matched package was tested and the score met `--min-score` (survivors do not change the exit status otherwise) |
| `1` | At least one package was not tested — the report covers the rest — or Kanly itself failed |
| `2` | Invalid flags or arguments |
| `3` | The score including not-covered mutants is below `--min-score` |

Progress goes to stderr (`--quiet` silences it), so stdout holds only the report.

### Worked example

```
$ kanly --quiet ./internal/runner/testdata/sample
Scope: ./internal/runner/testdata/sample

Survived (tests ran the mutated code but none failed):
  internal/runner/testdata/sample/sample.go:5:35 [int_arith] -→+ (id 2, fp fe04d7ed53a0)

Package: github.com/devenjarvis/kanly/internal/runner/testdata/sample | Total: 2 | Killed: 1 | Survived: 1 | Timeout: 0 | Not covered: 0 | Score: 50.0% (50.0% incl. not covered)

Total: 2 | Killed: 1 | Survived: 1 | Timeout: 0 | Not covered: 0 | Score: 50.0% (50.0% incl. not covered)

Top tests by kill count:
  github.com/devenjarvis/kanly/internal/runner/testdata/sample.TestAdd (1)

Tests that killed nothing (by itself no reason to delete a test):
  github.com/devenjarvis/kanly/internal/runner/testdata/sample.TestSub
```

The survived mutant on line 5 (`Sub`) means the test suite has a weak assertion on subtraction — it passes even when `-` is replaced with `+`. Mutants no test executes are listed separately under `Not covered`. `id` is the mutant's number for `--mutant`; `fp` is its [fingerprint](#accepting-mutants), which survives edits elsewhere in the package.

### Multi-package

Pass multiple patterns or use `./...` to mutate all packages in a module in a single run:

```
$ kanly --quiet ./internal/runner/testdata/sample ./internal/runner/testdata/cmpsample
...
Package: github.com/devenjarvis/kanly/internal/runner/testdata/cmpsample | Total: 4 | Killed: 4 | Survived: 0 | Timeout: 0 | Not covered: 0 | Score: 100.0% (100.0% incl. not covered)
Package: github.com/devenjarvis/kanly/internal/runner/testdata/sample | Total: 2 | Killed: 1 | Survived: 1 | Timeout: 0 | Not covered: 0 | Score: 50.0% (50.0% incl. not covered)

Total: 6 | Killed: 5 | Survived: 1 | Timeout: 0 | Not covered: 0 | Score: 83.3% (83.3% incl. not covered)
```

Each package's pipeline (rewrite → compile → baseline → mutant loop) runs independently. Packages with no test files or no mutations are skipped with a one-line stderr notice (unless `--quiet`) and the run continues. Within a package the per-test coverage pass and the mutant loop both run concurrently under a worker pool sized by `--jobs` (default `runtime.NumCPU()`; pass `--jobs=1` for serial execution). Cross-package parallelism is still sequential and is a planned future addition.

### Focused mutation

For tight LLM iteration loops, narrow the scope so the schema, test selection, and report stay sharp:

```
# Mutate one function; the report's test inventory drops tests that don't cover Add.
$ kanly ./internal/runner/testdata/sample:Add

# Multiple functions in one package, including a method.
$ kanly './internal/foo:Compute,(*Server).Handle'

# Pre-narrow the test inventory (skips baseline + coverage for everything else).
$ kanly --tests='^TestCompute' ./internal/foo:Compute

# Re-verify two survivors after editing the tests or code; stop each at its first failing test.
$ kanly --fail-fast --mutant=5e0d8f5590d5,45a983f95ba4 ./internal/foo

# Look at one mutant, then try a single test against it with go test.
$ kanly show ./internal/foo 5e0d8f5590d5
$ kanly test ./internal/foo 5e0d8f5590d5 -run TestCompute -v

# Compose with --diff: only changed lines inside the named function.
$ kanly --diff ./internal/foo:Compute
```

`--mutant` takes IDs or fingerprints. Mutant IDs are stable while the package's code is: the same source produces the same IDs on every run, and a scoped run (`--diff`, `pkg:Func`) reports the same ID for a mutant as a full run would. IDs are unique within a package only, so pass `--mutant` together with the package they came from. Editing a package's non-test code renumbers its mutants, but leaves the fingerprint of every mutant whose own line did not change, which is why the `--format=llm` brief's re-run commands use fingerprints. A fingerprint that matches no mutant is listed under `unmatched_mutants` in JSON and "Mutants not found" in the brief.

`kanly show <pkg> <mutant>...` prints each mutant's position, change and source line, with a caret under the mutated column and, for operator and literal mutants, the line as the mutant has it. `kanly test [--in=<pkg>] <pkg> <mutant> [go test flags]` runs `go test` on the package (or on the `--in` package, such as one that imports it) with that one mutant applied, so a test that kills the mutant fails; its exit status is go test's. Both rewrite the package afresh on every call, find mutants by fingerprint after edits, and take `--tags`.

When a scope is active (`pkg:Func`, `--tests`, `--mutant`, or `--diff`), the report's `scope` field and the text-mode `Scope:` header announce exactly what was tested. The inventory used for `zero_kill_tests` / `redundant_test_groups` is also narrowed to tests that touch a kept mutation, so the signal stays focused on the targeted change.

### Cross-package tests

By default only a package's own tests can kill its mutants, so code that is tested through another package shows up as not covered. With `--cross-package`, Kanly also finds the module's packages whose test binaries link the mutated package, directly or through others, compiles each one's tests against the mutant schema, runs them unmutated, and collects which of them run each line. Every covering test, the package's own and its dependents', then runs against each mutant; the mutant is killed when any of them fails. Dependents' tests are named by import path in `killing_tests` and `covering_tests` (`example.com/m/app.TestArea`), and the report sets `cross_package`. A dependent whose tests cannot be compiled or run unmutated is skipped with a warning; its failing tests are excluded as for the package's own.

Setting up a dependent costs a run of its tests plus one coverage run per test, for each mutated package. Kanly caches that setup in the user's cache directory (`$KANLY_CACHE_DIR` overrides it) under a key made of the two test binaries, which hold the mutated package's code but not its tests, the test flags, and the size and modification time of the module's files other than compiled Go code. Editing a package's tests and re-running it therefore reuses its dependents' setup; editing its code, a dependent, or a fixture under `testdata` does not. Only setups in which every test passed are cached. `--no-cache` turns the cache off.

### Mutation score

Kanly reports two scores:

- **Score** = (killed + timeout) / (total − not covered − not viable): how well the tests that run a piece of code check it.
- **Score incl. not covered** (`score_with_uncovered` in JSON) = (killed + timeout) / (total − not viable): untested code counts against it, so it cannot be raised by leaving code untested. `--min-score` gates on this one.

A mutant that makes the tests fail or hang has been detected. Timeouts are re-run alone with twice the budget before they count, so a slow, loaded machine does not turn surviving mutants into detected ones.

### Suppressing mutants

Some mutants cannot be killed because they do not change behaviour (equivalent mutants), and some code is not worth mutating. To keep the report actionable:

```go
// Every operator on this line:
x := a + b //kanly:ignore

// int_arith only, on the line after the directive, with a reason:
//kanly:ignore int_arith -- the buffer size is only a hint
n := size * 2

// In a function's doc comment, the whole function:
//
//kanly:ignore
func (c *Conn) Close() error { ... }
```

Operator names may be comma- or space-separated; text after `--` is a free-form reason. An unknown operator name is an error, so a typo cannot silently suppress nothing. Suppressed mutants are counted as `Ignored` in the report. Generated files (`// Code generated … DO NOT EDIT.`) are never mutated; `--exclude`, `--operators` and `--skip-operators` narrow a run from the command line. Suppression does not renumber mutants, so IDs still match a run without it.

#### Accepting mutants

A directive suppresses every mutant of an operator on a line. To accept one specific mutant (an equivalent one, or one that only a contrived test could kill) without touching the code, list its fingerprint in a `.kanlyignore` file in the package's directory, with a required reason:

```
# internal/diff/.kanlyignore
96e766e6b6d3 equivalent: a line without the "@@" prefix fails hunkRe, which requires it, and is skipped as before
de8f2a72b1e7 the larger scanner buffer only matters for diff lines over 64KB; a test for it would cost more than it checks
```

Every mutant carries a fingerprint (`fp` in text and `--format=llm` output, `fingerprint` in JSON). It is derived from the enclosing function, the operator, the change and the text of the mutated line, not from the mutant's ID or line number. Edits elsewhere in the package leave it alone. Editing the mutated line changes it, because the judgement that the mutant is harmless has to be made again. An entry that matches no mutant any more is reported as stale (`Stale .kanlyignore entries` in text and llm output, `stale_ignores` in JSON) so it can be deleted. A malformed line or a missing reason is an error, and so is a mutant ID written where the fingerprint belongs. Accepted mutants count as `Ignored`.

### Using Kanly in CI

```
kanly --quiet --diff --diff-base=origin/main --min-score=80 --format=sarif ./... > kanly.sarif
```

- `--diff` mutates only lines that changed since the branch diverged from `--diff-base` (the diff is taken against `git merge-base <base> HEAD`, so commits that landed on the base branch later are not counted), plus untracked files.
- `--min-score` fails the job (exit 3) when the changed code's mutants go undetected or untested.
- `--format=sarif` output can be uploaded with `github/codeql-action/upload-sarif` so survivors appear as annotations on the PR's changed lines. Run Kanly from the repository root so the SARIF paths resolve.
- Tests that share ports, files or databases may collide when Kanly runs them in parallel. Use `--verify-kills` to re-check kills one at a time, or `--jobs=1`.
- Mutants run in your checkout. Each test process gets a private `TMPDIR`; files created in the package directory or its `testdata` during the run are moved to a temporary quarantine directory afterwards (named on stderr, so nothing is lost), and files the tests modify or delete there are reported on stderr. Run Kanly in a clean checkout or a CI workspace you can discard.

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

`string_literal` also skips the help text passed as the `usage` argument to package `flag` (`fs.String("out", "-", "where to write")`): only a test pinning `--help` output could kill it.

Integer operators (`int_arith`, `int_cmp_*`, `int_bitwise`, `slice_index`, `slice_range`, `inc_dec`, `int_compound_assign`) target operands whose underlying type is a basic integer — `int`, sized signed/unsigned variants (`int8`/`int32`/`uint64`/…), `byte`, `rune`, `uintptr`, and named wrappers like `type UserID int64` or `time.Duration`. Binary forms additionally require both operands to share the same defined type (`types.Identical`), so `int + int32` is not mutated cross-type. Boolean operators (`bool_logic`, `bool_not`) follow the same policy for `bool` and named-bool wrappers like `type MyBool bool`; in a named-bool context they fire only when the (left) operand carries its own type, since an untyped operand such as `n < 1` would infer plain `bool`. Integer comparisons (`int_cmp_*`) return plain `bool` and are skipped in named-bool contexts like `var b MyBool = n < 5`. Type-bearing dispatchers are generic (`func __cMutInt[T __cInteger](a, b T, …) T`) so the rewritten code stays type-safe across the full family. `int_literal`, `bool_literal`, and `string_literal` are exceptions — they remain restricted to plain `int` / `bool` / `string` because the rewriter cannot infer the contextual type at an untyped-literal call site (so `var c Color = "red"` is not mutated); sized/named literal coverage is tracked in [Upcoming operators](#upcoming-operators). `err_return_nil` restricts to expressions whose static type is exactly the universe `error` interface (not concrete types like `*MyError`), and ignores naked `return` statements in functions with named results. `call_delete` targets `*ast.ExprStmt`-wrapped calls only (so `defer` and `go` statements are naturally excluded); it skips the builtins `panic`, `print`, and `println`, and `recover` (which only works when called directly by a deferred function, so it cannot be wrapped), but keeps `close`, `delete`, and `clear`, whose deletion produces observable behavior changes (hangs, residual map entries). Under an active mutant the call's argument expressions are not evaluated either — lazy semantics matching PIT's "Void Method Call Removal". `string_literal` skips empty strings, import paths, struct field tags, and const-decl initializers (each must syntactically remain a literal in Go and cannot be replaced by a function call). Literal operators also skip array lengths and array/slice composite-literal keys (`[]string{0: "a"}`), which must be constants. `slice_index` and `slice_range` exclude purely-constant indices. `inc_dec` and `int_compound_assign` use a shared closure-swap dispatcher so non-trivial LHS forms like `m[k]++` or `m[k] += y` are evaluated exactly once.

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

- **Narrow operator surface.** Today Kanly mutates arithmetic operators (expression and statement forms — `+ - * / %` and `+= -= *= /= %=`), integer comparisons, integer bitwise/shift operators (binary and compound), boolean logic, integer increment/decrement, integer-literal values, `error`-typed return values, full return-value zeroing for integer/string/bool/nilable types, void-discarded call statements, non-empty string literals, slice/map indices, and slice-range bounds. Float arithmetic and channel/switch operators are not yet covered — see [Upcoming operators](#upcoming-operators). Deleting side-effecting calls (e.g. `close(ch)`, locks, or sync primitives) can cause hangs in concurrent code, and inverting a loop counter (`i++→i--`) likewise loops forever; the per-mutant timeout — a few times the tests' normal run time, see `--timeout-factor` — bounds these and surfaces them as `Timeout` rather than `Survived`. A timeout counts as detected in the score, as in PIT and Stryker, once a re-run alone with twice the budget confirms it. When a test starts subprocesses, Kanly kills the whole process group on timeout. The baseline and coverage runs execute unmutated code and get at least 10 minutes. If a busy machine still produces timeouts for mutants that should be killed or survive, raise `--timeout-factor`.
- **Test-centric signals depend on operator coverage.** The `tests`, `zero_kill_tests`, and `survivors_by_function` views in the JSON report are only as sharp as the underlying operator set. Today, many entries in `zero_kill_tests` are tests exercising code that has no mutable operators in it — not tests with weak assertions. Read these views as exploration aids; broadening the operator set will turn them into a reliable test-quality signal.
- **Some assertions are structurally unkillable.** A few test patterns can't be expressed as MSG mutants no matter how many operators are added — most notably tests that assert on struct field tags (e.g., JSON tag names read via reflection), because Go syntax requires struct tags to be uninterpreted string literals that cannot be rewritten to runtime-conditional expressions. Such tests will always appear in `zero_kill_tests`; treat them as "structurally unkillable" rather than weak.
- **"Identical kill-sets" is not the same as "redundant tests."** Two tests can land in the same `redundant_test_groups` entry because they coincidentally trip the same mutated lines while asserting on entirely different concerns. Treat the grouping as a "look here" prompt, not a "delete one" recommendation.
- **Only a package's own tests can kill its mutants by default.** Code tested through another package (handler tests, end-to-end suites) shows up as not covered unless `--cross-package` is set; even then, tests outside the module, and tests that exercise the code through a separately built binary, do not count.
- **Sequential multi-package only.** `./...` and multiple positional args are supported; packages run sequentially. Within a package the per-test coverage pass and the mutant loop are already parallel (`--jobs N`, default `runtime.NumCPU()`); cross-package parallelism is tracked for a future release.

## Development

```bash
# Run all tests (including the slow e2e build+run test)
go test ./...

# Run unit tests only (skips e2e)
go test -short ./...

# Check the schema against pinned real-world modules (downloads them)
KANLY_CORPUS=1 go test -timeout 30m ./internal/corpus/

# Regenerate JSON golden files
GOLDEN_UPDATE=1 go test ./internal/report
```

Every pull request runs the [ci](.github/workflows/ci.yml) workflow: gofmt, `go vet`, staticcheck, `go test -race ./...`, and the real-world corpus. All of them must pass.

## Dogfooding

Every pull request on this repo runs Kanly against `./internal/...`, and again against just the lines the PR changes (`--diff`), via the [mutation-test](.github/workflows/mutation-test.yml) GitHub Actions workflow. Each result is posted as a PR comment (updated in place on re-pushes) and in the job summary. The diff job fails below 80% on the changed lines, counting mutants no test reaches, and its comment is the `--format=llm` brief, so an agent working on the PR can act on it directly; the full run fails below a floor that only moves up. Coding agents working in this repo get the same loop locally from the skill and Stop hook in `.claude/` (see [Coding agents](#coding-agents)).

**Fork PRs:** `GITHUB_TOKEN` on fork pull requests does not receive `pull-requests: write` permission, so the comment step will silently no-op for contributions from forks.

## Coding agents

Kanly carries its own instructions, so a repository does not need paragraphs of them in `AGENTS.md`:

```
kanly guide                    # print the workflow for any coding agent
kanly init --agent=claude      # write it as a Claude Code skill and register the stop hook
```

`kanly init --agent=claude` writes `.claude/skills/kanly/SKILL.md`, which Claude Code loads when it writes or changes Go code or tests, and adds a Stop hook to `.claude/settings.json` (other settings are kept; re-running updates the hook rather than adding another). Flags: `--command` (how to invoke Kanly, e.g. `go run ./cmd/kanly`), `--diff-base` (default `origin/main`), `--min-score` (default `80`), `--no-hook`, `--dir`. Delete the generated-file marker line in `SKILL.md` to keep local edits across re-runs. For other agents, one line in `AGENTS.md` is enough: "Before finishing a Go change, run `kanly guide` and follow it."

`kanly hook stop` is the hook. When the agent stops, it runs `kanly --diff --fail-fast --format=llm --llm-limit=10 --min-score=<n>` on what changed since `--diff-base`. If the score including not-covered mutants is below the threshold, it blocks the stop and hands the agent the brief (cut at 10,000 characters). It blocks once per stop: when the agent stops again, it is let through, so an agent that cannot reach the score is never trapped; CI stays the hard gate. If Kanly cannot test a package, the hook reports it on stderr without blocking. Turns that change no non-test Go file cost nothing. Arguments after `--` go to the Kanly run (`kanly hook stop -- --tags=integration`, or `-- --cross-package` for a module whose packages are tested through each other).

A score gate gives an agent a cheap way out: accepting every survivor in `.kanlyignore`. The guide forbids it, and reviewers should read `.kanlyignore` changes as closely as code.

## LLM-driven test repair

`--format=llm` emits a Markdown artifact designed to feed into an LLM that will work through the surviving mutants. Its layout is informed by Wang, Xu, Briand & Liu (2025): live (covered-but-survived) mutants and uncovered mutants get separate sections with different prompt-side guidance, because they need different fixes. Each operator class drops in a one-line strategy hint on its first occurrence (boundary values for `int_cmp_boundary`, sign-changing inputs for `int_arith`, and so on).

Not every survivor deserves a test, and a model told only to raise the score will write contorted tests for mutants that cannot or should not be killed. The artifact therefore offers three outcomes per mutant: **test it**, **simplify the code** (a survivor often reveals dead code, such as a field that is never read), or **accept it** in [`.kanlyignore`](#accepting-mutants) with a reason. It also asks for tests that check behaviour through the package's API rather than change-detector tests that pin error strings or internal details. Tests that killed nothing are listed, with a warning that this alone is no reason to delete them. The ones that ran a live mutant are marked, since only those point at a weak assertion.

To keep large runs affordable, each mutant takes one line: position within the function snippet, change, fingerprint, and at most five covering tests by short name. Functions with the most mutants come first, since one sharper test there settles the most, and the brief lists at most `--llm-limit` mutants (30 by default), naming the functions it leaves out. The artifact closes with a `kanly --fail-fast --mutant=<fingerprints>` command per package that re-runs only the listed mutants, which is the iterative verify-after-edit loop validated in the same paper, and points at `kanly test` for trying one test against one mutant.

## References

- DeMillo, R. A., Lipton, R. J., & Sayward, F. G. (1978). Hints on test data selection: Help for the practicing programmer. *IEEE Computer*, 11(4), 34–41.
- Offutt, A. J., & Untch, R. H. (2001). Mutation 2000: Uniting the orthogonal. In *Mutation Testing for the New Century* (pp. 34–44). Springer.
- Just, R., Jalali, D., Inozemtseva, L., Ernst, M. D., Holmes, R., & Fraser, G. (2014). Are mutants a valid substitute for real faults in software testing? In *Proceedings of FSE 2014* (pp. 654–665). ACM.
- Coles, H., Laurent, T., Henard, C., Papadakis, M., & Ventresque, A. (2016). PIT: A practical mutation testing tool for Java. In *Proceedings of ISSTA 2016* (pp. 449–452). ACM.
- Wang, G., Xu, Q., Briand, L., & Liu, K. (2025). Mutation-Guided Unit Test Generation with a Large Language Model. *arXiv preprint* [arXiv:2506.02954](https://arxiv.org/abs/2506.02954). Basis for the `--format=llm` artifact structure: live-vs-uncovered split, operator-class hints, and the `--mutant=<ids>` iterate-after-edit loop.
- PIT mutator reference: https://pitest.org/quickstart/mutators/
