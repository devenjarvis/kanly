package schema

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strings"
)

// errorMessageLits returns the string literals in file passed directly to
// errors.New or fmt.Errorf. A literal holding %w is left out: emptying it
// drops the wrapped error, which errors.Is can observe.
func errorMessageLits(file *ast.File, info *types.Info) map[ast.Node]bool {
	out := make(map[ast.Node]bool)
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || !isErrorConstructor(info, call) {
			return true
		}
		for _, arg := range call.Args {
			lit, ok := arg.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				continue
			}
			if !strings.Contains(constant.StringVal(info.Types[lit].Value), "%w") {
				out[lit] = true
			}
		}
		return true
	})
	return out
}

func isErrorConstructor(info *types.Info, call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	fn, ok := info.Uses[sel.Sel].(*types.Func)
	return ok && (fn.FullName() == "errors.New" || fn.FullName() == "fmt.Errorf")
}
