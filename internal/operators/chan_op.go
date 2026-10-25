package operators

import (
	"go/ast"
	"go/token"
	"go/types"

	"github.com/devenjarvis/kanly/internal/mutation"
)

// ChanOp removes channel operations: a receive `<-ch` becomes the zero value
// of the element type without touching the channel, and a send statement
// `ch <- x` does nothing (neither ch nor x is evaluated). close(ch) is a call
// and belongs to call_delete.
//
// Skipped:
//   - The communication of a select case (`case v := <-ch:`, `case ch <- v:`),
//     which Go requires to be a plain send or receive.
//   - The comma-ok receive `v, ok := <-ch`, whose second value only a receive
//     expression can produce.
//   - Receives from a channel whose type is a type parameter, which the
//     generic dispatcher cannot infer an element type from.
//
// Rewrite: a receive becomes __cMutRecv(ch, mutIDs...), which infers the
// element type from ch; a send becomes __cMutCallSkip(func() { ch <- x }, ...).
// A receive used as a statement (`<-done`) is a receive like any other; its
// mutant stops waiting.
type ChanOp struct{}

func (ChanOp) Name() string          { return "chan_op" }
func (ChanOp) DispatcherKey() string { return "chan_op" }

func (ChanOp) Find(file *ast.File, info *types.Info) []mutation.Candidate {
	skip := make(map[ast.Node]bool)
	ast.Inspect(file, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.CommClause:
			if s.Comm != nil {
				markCommOp(s.Comm, skip)
			}
		case *ast.AssignStmt:
			if len(s.Lhs) == 2 && len(s.Rhs) == 1 {
				skip[ast.Unparen(s.Rhs[0])] = true
			}
		case *ast.ValueSpec:
			if len(s.Names) == 2 && len(s.Values) == 1 {
				skip[ast.Unparen(s.Values[0])] = true
			}
		}
		return true
	})

	var candidates []mutation.Candidate
	ast.Inspect(file, func(n ast.Node) bool {
		if skip[n] {
			return true
		}
		switch x := n.(type) {
		case *ast.UnaryExpr:
			if x.Op != token.ARROW || !recvInferable(info, x.X) {
				return true
			}
			candidates = append(candidates, mutation.Candidate{
				Node:     x,
				Pos:      x.OpPos,
				Original: "<-" + types.ExprString(x.X),
				Mutant:   "zero",
			})
		case *ast.SendStmt:
			candidates = append(candidates, mutation.Candidate{
				Node:     x,
				Pos:      x.Arrow,
				Original: types.ExprString(x.Chan) + " <- ...",
				Mutant:   "",
			})
		}
		return true
	})
	return candidates
}

// markCommOp marks the send or receive a select case communicates with.
func markCommOp(comm ast.Stmt, skip map[ast.Node]bool) {
	switch s := comm.(type) {
	case *ast.SendStmt:
		skip[s] = true
	case *ast.ExprStmt:
		skip[ast.Unparen(s.X)] = true
	case *ast.AssignStmt:
		if len(s.Rhs) == 1 {
			skip[ast.Unparen(s.Rhs[0])] = true
		}
	}
}

// recvInferable reports whether ch's element type can be inferred by the
// generic __cMutRecv: ch's type must be a channel type, not a type parameter
// constrained to one.
func recvInferable(info *types.Info, ch ast.Expr) bool {
	t := info.TypeOf(ch)
	if t == nil {
		return false
	}
	if _, ok := t.(*types.TypeParam); ok {
		return false
	}
	_, ok := t.Underlying().(*types.Chan)
	return ok
}

func (ChanOp) Rewrite(c mutation.Candidate, mutIDs []int) ast.Node {
	if n, ok := c.Node.(*ast.UnaryExpr); ok {
		return idCall("__cMutRecv", []ast.Expr{n.X}, mutIDs)
	}
	n := c.Node.(*ast.SendStmt)
	send := stmtFuncLit(&ast.SendStmt{Chan: n.Chan, Value: n.Value})
	return &ast.ExprStmt{X: idCall("__cMutCallSkip", []ast.Expr{send}, mutIDs)}
}
