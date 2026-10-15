package operators

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"github.com/devenjarvis/kanly/internal/mutation"
)

// StringLiteral mutates every non-empty string literal to "".
//
// Skipped positions (would produce non-compilable Go if rewritten to a function call):
//   - Empty strings ("") — mutation is a no-op.
//   - Import paths (e.g. import "fmt") — must be string literals at parse time.
//   - Struct field tags (e.g. `json:"f"`) — must be string literals at parse time.
//   - Const-decl initializers (e.g. const X = "alice") — must be constant expressions.
//   - Array lengths and array/slice composite-literal keys (e.g. [len("ab")]int)
//     — must be constant expressions.
//
// Strict-identity type policy, as for int_literal: the literal's contextual
// type must be plain string. __cMutString returns string, so a literal used as
// a named string type (`var c Color = "red"`, `case "red":` in a switch on a
// Color) would no longer type-check.
type StringLiteral struct{}

func (StringLiteral) Name() string          { return "string_literal" }
func (StringLiteral) DispatcherKey() string { return "string_literal" }

// Rewrite returns __cMutString(<orig BasicLit>, mutIDs...).
func (StringLiteral) Rewrite(c mutation.Candidate, mutIDs []int) ast.Node {
	lit := c.Node.(*ast.BasicLit)
	args := []ast.Expr{lit}
	for _, id := range mutIDs {
		args = append(args, &ast.BasicLit{Kind: token.INT, Value: fmt.Sprintf("%d", id)})
	}
	return &ast.CallExpr{Fun: &ast.Ident{Name: "__cMutString"}, Args: args}
}

func (StringLiteral) Find(file *ast.File, info *types.Info) []mutation.Candidate {
	var candidates []mutation.Candidate
	var stack []ast.Node

	ast.Inspect(file, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}

		if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			if lit.Value != `""` && lit.Value != "``" && stringLitMutable(stack, lit) &&
				!inConstantPosition(info, stack, lit) && isPlainStringLit(info, lit) {
				candidates = append(candidates, mutation.Candidate{
					Node:     lit,
					Pos:      lit.Pos(),
					Original: lit.Value,
					Mutant:   `""`,
				})
			}
		}

		stack = append(stack, n)
		return true
	})

	return candidates
}

// isPlainStringLit reports whether lit's contextual type is exactly string
// (or still untyped string).
func isPlainStringLit(info *types.Info, lit *ast.BasicLit) bool {
	tv, ok := info.Types[lit]
	if !ok {
		return false
	}
	return tv.Type == types.Typ[types.String] || tv.Type == types.Typ[types.UntypedString]
}

// stringLitMutable reports whether the literal can be safely replaced with a
// function call expression. It rejects import paths, struct field tags, and
// const-decl initializers, which must syntactically remain literal.
func stringLitMutable(stack []ast.Node, lit *ast.BasicLit) bool {
	for i := len(stack) - 1; i >= 0; i-- {
		switch p := stack[i].(type) {
		case *ast.ImportSpec:
			return false
		case *ast.Field:
			if p.Tag == lit {
				return false
			}
		case *ast.GenDecl:
			if p.Tok == token.CONST {
				return false
			}
		}
	}
	return true
}
