package operators

import (
	"go/ast"
	"go/types"
)

// intOperands reports whether x and y are both integer-typed and at least one
// is not a compile-time constant. Accepts every basic type whose Info bit
// includes IsInteger — `int`, sized ints (`int8`/`int16`/`int32`/`int64`),
// `uint` and its sized variants, `uintptr`, `byte`, `rune` — as well as any
// named wrapper around them (`type MyInt int`, `time.Duration`, etc.).
//
// Both operands must share the same defined type (types.Identical), which
// Go's type-checker already enforces for non-constant binary operands —
// the check is kept here as defensive backstop, so a future caller cannot
// inadvertently mutate a cross-typed binary expression.
func intOperands(info *types.Info, x, y ast.Expr) bool {
	lv, lok := info.Types[x]
	rv, rok := info.Types[y]
	if !lok || !rok {
		return false
	}
	if lv.Value != nil && rv.Value != nil {
		return false
	}
	if !isIntegerType(lv.Type) || !isIntegerType(rv.Type) {
		return false
	}
	return types.Identical(lv.Type, rv.Type)
}

// boolOperands reports whether x and y are both boolean-typed and at least
// one is not a compile-time constant. Accepts plain `bool` and any named
// wrapper around it (`type MyBool bool`). Both operands must share the
// same defined type.
func boolOperands(info *types.Info, x, y ast.Expr) bool {
	lv, lok := info.Types[x]
	rv, rok := info.Types[y]
	if !lok || !rok {
		return false
	}
	if lv.Value != nil && rv.Value != nil {
		return false
	}
	if !isBoolType(lv.Type) || !isBoolType(rv.Type) {
		return false
	}
	return types.Identical(lv.Type, rv.Type)
}

// boolOperand reports whether x is a non-constant boolean-typed expression.
// Accepts plain `bool` and named wrappers like `type MyBool bool`.
func boolOperand(info *types.Info, x ast.Expr) bool {
	lv, lok := info.Types[x]
	if !lok {
		return false
	}
	if lv.Value != nil {
		return false
	}
	return isBoolType(lv.Type)
}

// intOperand reports whether x is a non-constant integer-typed expression.
// Accepts every IsInteger basic, plus named wrappers around them.
func intOperand(info *types.Info, x ast.Expr) bool {
	lv, lok := info.Types[x]
	if !lok {
		return false
	}
	if lv.Value != nil {
		return false
	}
	return isIntegerType(lv.Type)
}

// isIntegerType reports whether t's underlying type is a basic integer
// (signed or unsigned, any width, including byte/rune/uintptr).
func isIntegerType(t types.Type) bool {
	if t == nil {
		return false
	}
	b, ok := t.Underlying().(*types.Basic)
	if !ok {
		return false
	}
	return b.Info()&types.IsInteger != 0
}

// isBoolType reports whether t's underlying type is the boolean basic.
func isBoolType(t types.Type) bool {
	if t == nil {
		return false
	}
	b, ok := t.Underlying().(*types.Basic)
	if !ok {
		return false
	}
	return b.Info()&types.IsBoolean != 0
}

// errorType is the universe "error" interface, captured once.
var errorType = types.Universe.Lookup("error").Type()

// isErrorType reports whether x's static type is exactly the universe error interface.
// Named error wrappers (e.g. "*MyError") are excluded, mirroring the strict-identity
// policy used by err_return_nil.
func isErrorType(info *types.Info, x ast.Expr) bool {
	tv, ok := info.Types[x]
	if !ok || tv.Type == nil {
		return false
	}
	return types.Identical(tv.Type, errorType)
}

// isNilableType reports whether values of t accept the untyped nil constant.
// Used by ReturnZero to recognise pointer/slice/map/chan/func/interface return
// types — including named wrappers like `type MyMap map[string]int` or
// `io.Reader`. The universe `error` interface is excluded — ErrReturnNil
// handles it.
func isNilableType(t types.Type) bool {
	switch t.Underlying().(type) {
	case *types.Pointer, *types.Slice, *types.Map, *types.Chan, *types.Signature:
		return true
	case *types.Interface:
		return !types.Identical(t, errorType)
	}
	return false
}
