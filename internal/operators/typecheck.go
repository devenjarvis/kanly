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
