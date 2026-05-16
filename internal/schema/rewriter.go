package schema

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"

	"golang.org/x/tools/go/ast/astutil"

	"github.com/devenjarvis/cauldron/internal/mutation"
	"github.com/devenjarvis/cauldron/internal/source"
)

// Rewritten holds the results of schema rewriting: rewritten file sources and the dispatcher.
type Rewritten struct {
	Files      map[string]string // absolute file path → rewritten source
	Dispatcher string            // source of the generated cauldron_schema.go file
	Mutations  []mutation.Mutation
}

// opcodeConst maps a token to the __cXxx constant name in the dispatcher.
var opcodeConst = map[token.Token]string{
	token.ADD: "__cAdd",
	token.SUB: "__cSub",
	token.MUL: "__cMul",
	token.QUO: "__cQuo",
}

// Rewrite transforms pkg's source files into a mutant schema using the provided operators.
func Rewrite(pkg *source.Package, ops []mutation.Operator) (*Rewritten, error) {
	mutID := 0
	var mutations []mutation.Mutation
	rewrittenFiles := make(map[string]string)

	for filePath, file := range pkg.Files {
		// Collect all candidates for this file across all operators.
		type entry struct {
			node    *ast.BinaryExpr
			cand    mutation.Candidate
			id      int
		}
		var entries []entry

		for _, op := range ops {
			for _, c := range op.Find(file, pkg.TypesInfo) {
				mutID++
				pos := pkg.Fset.Position(c.Pos)
				mutations = append(mutations, mutation.Mutation{
					ID:           mutID,
					File:         filePath,
					Line:         pos.Line,
					Column:       pos.Column,
					OperatorName: op.Name(),
					Original:     c.Original,
					Mutant:       c.Mutant,
				})
				entries = append(entries, entry{
					node: c.Node.(*ast.BinaryExpr),
					cand: c,
					id:   mutID,
				})
			}
		}

		if len(entries) == 0 {
			continue
		}

		// Build a set for fast lookup: *ast.BinaryExpr → (mutation ID, original op).
		type rewriteInfo struct {
			mutID int
			origTok token.Token
		}
		nodeMap := make(map[*ast.BinaryExpr]rewriteInfo, len(entries))
		for _, e := range entries {
			nodeMap[e.node] = rewriteInfo{mutID: e.id, origTok: e.node.Op}
		}

		// Clone the file AST by applying a cursor that replaces matching BinaryExprs.
		newFile := astutil.Apply(file, func(cursor *astutil.Cursor) bool {
			expr, ok := cursor.Node().(*ast.BinaryExpr)
			if !ok {
				return true
			}
			info, ok := nodeMap[expr]
			if !ok {
				return true
			}
			opcode, ok := opcodeConst[info.origTok]
			if !ok {
				return true
			}
			call := &ast.CallExpr{
				Fun: &ast.Ident{Name: "__cMutInt"},
				Args: []ast.Expr{
					expr.X,
					expr.Y,
					&ast.Ident{Name: opcode},
					&ast.BasicLit{Kind: token.INT, Value: fmt.Sprintf("%d", info.mutID)},
				},
			}
			cursor.Replace(call)
			return true
		}, nil)

		var buf bytes.Buffer
		if err := format.Node(&buf, pkg.Fset, newFile); err != nil {
			return nil, fmt.Errorf("format %s: %w", filePath, err)
		}
		rewrittenFiles[filePath] = buf.String()
	}

	dispatcher, err := RenderDispatcher(pkg.Pkg.Name(), mutations)
	if err != nil {
		return nil, fmt.Errorf("render dispatcher: %w", err)
	}

	return &Rewritten{
		Files:      rewrittenFiles,
		Dispatcher: dispatcher,
		Mutations:  mutations,
	}, nil
}
