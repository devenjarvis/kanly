# Kanly mutation report

**Scope:** `./...`

## Summary

| Total | Killed | Survived | Timeout | NotCovered | NotViable | Score |
|-------|--------|----------|---------|------------|-----------|-------|
| 3 | 1 | 1 | 0 | 1 | 0 | 50.0% |

## Surviving mutants (offense targets)

### example.com/pkg/bar — Mul  (1 unkilled)

**Source: bar.go:3-3**

```go
3  func Mul(a, b int) int { return a * b }
```

- **#3** at bar.go:3:15 — `int_arith` `*` → `/` _(not_covered)_
  - Covering tests that did NOT kill: _none — mutation site is not exercised by any test_

### example.com/pkg/foo — Add  (1 unkilled)

**Source: foo.go:3-6**

```go
3  func Add(a, b int) int {
4  	// no-op comment
5  	return a + b + 1
6  }
```

- **#2** at foo.go:5:14 — `int_literal` `1` → `0` _(survived)_
  - Covering tests that did NOT kill: `example.com/pkg/foo.TestAdd`, `example.com/pkg/foo.TestAddSmoke`

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

