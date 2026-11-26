package schema

import (
	"go/ast"
	"go/constant"
	"go/types"
)

const reasonZeroBesideError = "zero value returned beside a non-nil error"

// zeroBesideError returns the zero-value literals (0, false, "", nil) that a
// return statement in file returns beside an error result that is not the
// literal nil. Callers almost always ignore the other results when err is
// non-nil, so mutating such a literal rarely changes what they see; it can
// (io.Reader.Read returns n > 0 with an error), so these mutants are marked,
// not dropped.
func zeroBesideError(file *ast.File, info *types.Info) map[ast.Node]bool {
	out := make(map[ast.Node]bool)
	// sigs holds, for each node on the inspection path, the signature of
	// the innermost function around it, so each return meets its own.
	sigs := []*types.Signature{nil}
	ast.Inspect(file, func(n ast.Node) bool {
		if n == nil {
			sigs = sigs[:len(sigs)-1]
			return true
		}
		sig := sigs[len(sigs)-1]
		switch x := n.(type) {
		case *ast.FuncDecl:
			sig, _ = info.TypeOf(x.Name).(*types.Signature)
		case *ast.FuncLit:
			sig, _ = info.TypeOf(x).(*types.Signature)
		case *ast.ReturnStmt:
			markZeroBesideError(x, sig, info, out)
		}
		sigs = append(sigs, sig)
		return true
	})
	return out
}

var errorType = types.Universe.Lookup("error").Type()

// markZeroBesideError adds ret's zero-value literal results to out when one
// of its error results is not the literal nil. A return of a multi-value
// call has fewer expressions than sig has results; it holds no literal.
func markZeroBesideError(ret *ast.ReturnStmt, sig *types.Signature, info *types.Info, out map[ast.Node]bool) {
	if sig == nil {
		return
	}
	besideErr := false
	for i, r := range ret.Results {
		if types.Identical(sig.Results().At(i).Type(), errorType) && !isZeroLiteral(info, r) {
			besideErr = true
		}
	}
	if !besideErr {
		return
	}
	for _, r := range ret.Results {
		if isZeroLiteral(info, r) {
			out[r] = true
		}
	}
}

// isZeroLiteral reports whether e is a literal spelling of its type's zero
// value: 0, 0.0, "", the predeclared false, or nil.
func isZeroLiteral(info *types.Info, e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.BasicLit:
	case *ast.Ident:
		obj := info.Uses[x]
		if _, ok := obj.(*types.Nil); ok {
			return true
		}
		if obj != types.Universe.Lookup("false") {
			return false
		}
	default:
		return false
	}
	v := info.Types[e].Value
	switch v.Kind() {
	case constant.Bool:
		return !constant.BoolVal(v)
	case constant.String:
		return constant.StringVal(v) == ""
	}
	return constant.Sign(v) == 0
}
