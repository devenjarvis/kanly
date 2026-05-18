package operators

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strconv"

	"github.com/devenjarvis/kanly/internal/mutation"
)

// IntLiteral mutates every typed-int basic literal to a small set of canonical
// alternates: 0, 1, -n, n+1, n-1 (skipping no-ops where the mutant value equals
// the original). Magic-number bugs and off-by-one errors live in literals;
// weak tests that don't pin exact values won't kill these mutants.
//
// Skipped positions (would not compile or are not user-meaningful):
//   - Import specs (no int literals there, but defensive).
//   - Const declarations — initializers must be constant expressions.
//   - Struct field tags (string-only, but defensive).
//   - Array type lengths (e.g. `[10]int{}`) — must be a constant expression.
//
// Strict-identity type policy: the literal's typed value in context must be
// exactly types.Typ[types.Int] — named ints (`type MyInt int`), sized ints
// (`int64`, `int32`, etc.), and untyped contexts (const, array size) are all
// excluded automatically by this check.
type IntLiteral struct{}

func (IntLiteral) Name() string          { return "int_literal" }
func (IntLiteral) DispatcherKey() string { return "int_literal" }

// Rewrite returns __cMutIntLit(<orig BasicLit>, mutIDs...).
func (IntLiteral) Rewrite(c mutation.Candidate, mutIDs []int) ast.Node {
	lit := c.Node.(*ast.BasicLit)
	args := []ast.Expr{lit}
	for _, id := range mutIDs {
		args = append(args, &ast.BasicLit{Kind: token.INT, Value: fmt.Sprintf("%d", id)})
	}
	return &ast.CallExpr{Fun: &ast.Ident{Name: "__cMutIntLit"}, Args: args}
}

func (IntLiteral) Find(file *ast.File, info *types.Info) []mutation.Candidate {
	var candidates []mutation.Candidate
	var stack []ast.Node

	ast.Inspect(file, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.INT {
			if intLitMutable(stack) && intLitOperand(info, lit) {
				for _, m := range intLitMutants(lit.Value) {
					candidates = append(candidates, mutation.Candidate{
						Node:     lit,
						Pos:      lit.Pos(),
						Original: lit.Value,
						Mutant:   m,
					})
				}
			}
		}
		stack = append(stack, n)
		return true
	})
	return candidates
}

// intLitOperand reports whether lit's contextual type is exactly types.Int.
// Unlike intOperand it ACCEPTS compile-time constants — every int literal is one.
func intLitOperand(info *types.Info, lit *ast.BasicLit) bool {
	tv, ok := info.Types[lit]
	if !ok {
		return false
	}
	return tv.Type == types.Typ[types.Int]
}

// intLitMutable reports whether the literal can be safely replaced with a
// function call. Rejects const-decl initializers (must be const exprs),
// array type lengths (must be const), and the rare struct-tag / import-spec
// positions (defensive — int literals don't normally appear there).
func intLitMutable(stack []ast.Node) bool {
	for i := len(stack) - 1; i >= 0; i-- {
		switch p := stack[i].(type) {
		case *ast.ImportSpec:
			return false
		case *ast.ArrayType:
			return false
		case *ast.GenDecl:
			if p.Tok == token.CONST {
				return false
			}
		}
	}
	return true
}

// intLitMutants returns the deduplicated mutant int values for orig (as Go source).
// orig is in any int-literal form recognized by strconv.ParseInt with base 0:
// decimal, 0x.., 0o.., 0b.., underscore-separated. The output is always decimal.
func intLitMutants(orig string) []string {
	n, err := strconv.ParseInt(orig, 0, 64)
	if err != nil {
		return nil
	}
	seen := map[int64]bool{n: true}
	var out []string
	consider := func(v int64) {
		if seen[v] {
			return
		}
		seen[v] = true
		out = append(out, strconv.FormatInt(v, 10))
	}
	consider(0)
	consider(1)
	consider(-n)
	consider(n + 1)
	consider(n - 1)
	return out
}
