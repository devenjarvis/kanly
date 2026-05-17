package operators

import (
	"go/ast"
	"go/types"
)

// intOperands reports whether both x and y are exactly types.Int (not named or sized variants)
// and at least one is not a compile-time constant.
// Do NOT use .Underlying() — named types like "type MyInt int" must be excluded.
func intOperands(info *types.Info, x, y ast.Expr) bool {
	lv, lok := info.Types[x]
	rv, rok := info.Types[y]
	if !lok || !rok {
		return false
	}
	if lv.Value != nil && rv.Value != nil {
		return false
	}
	return lv.Type == types.Typ[types.Int] && rv.Type == types.Typ[types.Int]
}

// boolOperands reports whether both x and y are exactly types.Bool (not named types or untyped bool)
// and at least one is not a compile-time constant.
// Do NOT use .Underlying() — named types like "type MyBool bool" must be excluded.
func boolOperands(info *types.Info, x, y ast.Expr) bool {
	lv, lok := info.Types[x]
	rv, rok := info.Types[y]
	if !lok || !rok {
		return false
	}
	if lv.Value != nil && rv.Value != nil {
		return false
	}
	return lv.Type == types.Typ[types.Bool] && rv.Type == types.Typ[types.Bool]
}

// boolOperand reports whether x is exactly types.Bool (not named types or untyped bool)
// and is not a compile-time constant.
// Do NOT use .Underlying() — named types like "type MyBool bool" must be excluded.
func boolOperand(info *types.Info, x ast.Expr) bool {
	lv, lok := info.Types[x]
	if !lok {
		return false
	}
	if lv.Value != nil {
		return false
	}
	return lv.Type == types.Typ[types.Bool]
}

// errorType is the universe "error" interface, captured once.
var errorType = types.Universe.Lookup("error").Type()

// isErrorType reports whether x's static type is exactly the universe error interface.
// Named error wrappers (e.g. "*MyError") are excluded, mirroring the strict-identity
// policy used by intOperands and boolOperands.
func isErrorType(info *types.Info, x ast.Expr) bool {
	tv, ok := info.Types[x]
	if !ok || tv.Type == nil {
		return false
	}
	return types.Identical(tv.Type, errorType)
}
