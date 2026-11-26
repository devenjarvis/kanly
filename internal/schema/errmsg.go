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
			if !wrapsError(constant.StringVal(info.Types[lit].Value)) {
				out[lit] = true
			}
		}
		return true
	})
	return out
}

func isErrorConstructor(info *types.Info, call *ast.CallExpr) bool {
	var id *ast.Ident
	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.SelectorExpr:
		id = fun.Sel
	case *ast.Ident:
		id = fun
	default:
		return false
	}
	fn, ok := info.Uses[id].(*types.Func)
	return ok && (fn.FullName() == "errors.New" || fn.FullName() == "fmt.Errorf")
}

// wrapsError reports whether format holds a %w verb, with any flags,
// width, precision or argument index between % and w. %% is a literal
// percent sign.
func wrapsError(format string) bool {
	for i := 0; i < len(format); i++ {
		if format[i] != '%' {
			continue
		}
		j := i + 1
		for j < len(format) && strings.IndexByte("+-# 0123456789.*[]", format[j]) >= 0 {
			j++
		}
		if j < len(format) && format[j] == 'w' {
			return true
		}
		if j < len(format) && format[j] == '%' && j == i+1 {
			j++
		}
		i = j - 1
	}
	return false
}
