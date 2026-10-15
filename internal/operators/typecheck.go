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
// Accepts every IsInteger basic, plus named wrappers around them. Type
// expressions are rejected: in `F[int](x)` or `Box[int]` the index is a type
// argument, and wrapping it in a dispatcher call is a syntax error.
func intOperand(info *types.Info, x ast.Expr) bool {
	lv, lok := info.Types[x]
	if !lok {
		return false
	}
	if lv.Value != nil || lv.IsType() {
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

// inConstantPosition reports whether n sits somewhere Go requires a constant
// expression, so replacing any part of it with a dispatcher call would not
// compile: an array length (`[len("abc")]int`) or the key of an array or
// slice composite literal (`[]string{0: "a"}`). stack holds n's ancestors,
// outermost first, as built by the operators' ast.Inspect walks.
func inConstantPosition(info *types.Info, stack []ast.Node, n ast.Node) bool {
	child := n
	for i := len(stack) - 1; i >= 0; i-- {
		switch p := stack[i].(type) {
		case *ast.ArrayType:
			if p.Len == child {
				return true
			}
		case *ast.KeyValueExpr:
			if p.Key == child && i > 0 {
				if cl, ok := stack[i-1].(*ast.CompositeLit); ok && isArrayOrSlice(info.TypeOf(cl)) {
					return true
				}
			}
		}
		child = stack[i]
	}
	return false
}

func isArrayOrSlice(t types.Type) bool {
	if t == nil {
		return false
	}
	switch t.Underlying().(type) {
	case *types.Array, *types.Slice:
		return true
	}
	return false
}

// isPlainBoolResult reports whether e's contextual type is plain bool (or
// still untyped bool). Comparisons are untyped and take on the type their
// context demands, so `var b MyBool = n < 5` records e as MyBool.
func isPlainBoolResult(info *types.Info, e ast.Expr) bool {
	tv, ok := info.Types[e]
	if !ok {
		return false
	}
	return tv.Type == types.Typ[types.Bool] || tv.Type == types.Typ[types.UntypedBool]
}

// boolResultInferable reports whether a generic bool dispatcher whose type
// parameter is inferred from operand would return expr's contextual type.
// That holds when the context is plain bool, or when operand carries its own
// type; an untyped operand (a comparison, `!(a < b)`, an untyped constant)
// infers plain bool, which no longer fits a named-bool context.
func boolResultInferable(info *types.Info, expr, operand ast.Expr) bool {
	return isPlainBoolResult(info, expr) || hasOwnType(info, operand)
}

// hasOwnType reports whether e's static type is fixed by e itself rather
// than by its context, i.e. e is not an untyped expression.
func hasOwnType(info *types.Info, e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.ParenExpr:
		return hasOwnType(info, x.X)
	case *ast.Ident:
		return objHasOwnType(info.ObjectOf(x))
	case *ast.SelectorExpr:
		return objHasOwnType(info.ObjectOf(x.Sel))
	case *ast.CallExpr, *ast.IndexExpr, *ast.IndexListExpr, *ast.StarExpr, *ast.TypeAssertExpr:
		return true
	}
	return false
}

func objHasOwnType(obj types.Object) bool {
	if obj == nil {
		return false
	}
	b, ok := obj.Type().(*types.Basic)
	return !ok || b.Info()&types.IsUntyped == 0
}

// containsRecover reports whether e calls the recover builtin. recover only
// stops a panic when called directly by the deferred function, so moving such
// a call into a dispatcher closure silently breaks it.
func containsRecover(info *types.Info, e ast.Node) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || found {
			return !found
		}
		if id, ok := call.Fun.(*ast.Ident); ok {
			if b, ok := info.Uses[id].(*types.Builtin); ok && b.Name() == "recover" {
				found = true
			}
		}
		return !found
	})
	return found
}
