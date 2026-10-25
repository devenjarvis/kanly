package operators

import (
	"bytes"
	"go/ast"
	"go/printer"
	"go/token"
	"go/types"
	"strconv"
	"strings"

	"github.com/devenjarvis/kanly/internal/mutation"
)

// SwitchCase mutates expression switches two ways: it drops a case clause,
// so a value it matched falls to the clauses after it or to default, and it
// swaps the bodies of adjacent clauses. Type switches are skipped: their
// clauses bind a differently typed variable each, so neither mutant
// compiles in general.
//
// Rewrite: the switch is rewritten once, whatever its mutants. Its tag
// becomes a closure that evaluates the case expressions in Go's order
// (the tag first, then each case top to bottom, left to right) and returns
// the index of the clause that matched; a dropped clause is skipped without
// evaluating its expressions. Each clause's expression list becomes its
// index, so the bodies, fallthrough, break and the terminating-statement
// rules are untouched:
//
//	switch init; __cMutSwap(func() int {
//		__kanlyTag := tag
//		if __cMutKeep(3) {
//			if __kanlyTag == a { return 0 }
//		}
//		...
//		return <default index, or -1>
//	}(), 0, 1, 4) {
//	case 0:
//		...
//
// Skipped: switches whose tag or case expressions call recover, which only
// works when called directly by a deferred function.
type SwitchCase struct{}

func (SwitchCase) Name() string          { return "switch_case" }
func (SwitchCase) DispatcherKey() string { return "switch_case" }

// switchSwapMutant prefixes the Mutant of a swap candidate; a drop
// candidate's Mutant is empty.
const switchSwapMutant = "swap with "

func (SwitchCase) Find(file *ast.File, info *types.Info) []mutation.Candidate {
	var candidates []mutation.Candidate
	ast.Inspect(file, func(n ast.Node) bool {
		s, ok := n.(*ast.SwitchStmt)
		if !ok {
			return true
		}
		if s.Tag != nil && containsRecover(info, s.Tag) {
			return true
		}
		clauses := make([]*ast.CaseClause, len(s.Body.List))
		hasDefault := false
		for i, st := range s.Body.List {
			clauses[i] = st.(*ast.CaseClause)
			if clauses[i].List == nil {
				hasDefault = true
			}
			for _, e := range clauses[i].List {
				if containsRecover(info, e) {
					return true
				}
			}
		}
		for i, c := range clauses {
			if c.List == nil {
				continue
			}
			// Dropping an empty last clause changes nothing when no
			// default would run instead.
			if len(c.Body) == 0 && !hasDefault && i == len(clauses)-1 {
				continue
			}
			candidates = append(candidates, mutation.Candidate{
				Node:     s,
				Pos:      c.Case,
				Original: clauseLabel(c),
				Mutant:   "",
			})
		}
		for i := 0; i+1 < len(clauses); i++ {
			if bodyText(clauses[i].Body) == bodyText(clauses[i+1].Body) {
				continue
			}
			candidates = append(candidates, mutation.Candidate{
				Node:     s,
				Pos:      clauses[i].Case,
				Original: clauseLabel(clauses[i]),
				Mutant:   switchSwapMutant + clauseLabel(clauses[i+1]),
			})
		}
		return true
	})
	return candidates
}

// clauseLabel renders a clause's header for the report, shortened when long.
func clauseLabel(c *ast.CaseClause) string {
	if c.List == nil {
		return "default"
	}
	exprs := make([]string, len(c.List))
	for i, e := range c.List {
		exprs[i] = types.ExprString(e)
	}
	label := "case " + strings.Join(exprs, ", ")
	if r := []rune(label); len(r) > 40 {
		label = string(r[:39]) + "…"
	}
	return label
}

// bodyText prints a clause body so identical bodies, whose swap is an
// equivalent mutant, can be recognised.
func bodyText(body []ast.Stmt) string {
	var buf bytes.Buffer
	// Printing to a buffer fails only on a malformed AST, which the type
	// checker has already ruled out.
	_ = printer.Fprint(&buf, token.NewFileSet(), &ast.BlockStmt{List: body})
	return buf.String()
}

// Rewrite rewrites the switch for a single candidate; see RewriteGroup.
func (op SwitchCase) Rewrite(c mutation.Candidate, mutIDs []int) ast.Node {
	return op.RewriteGroup([]mutation.Candidate{c}, mutIDs)
}

// RewriteGroup rewrites the switch for every candidate on it at once; each
// candidate's Pos is the `case` keyword of the clause it drops, or of the
// first of the two clauses it swaps.
func (SwitchCase) RewriteGroup(cands []mutation.Candidate, mutIDs []int) ast.Node {
	s := cands[0].Node.(*ast.SwitchStmt)
	index := make(map[token.Pos]int)
	for i, st := range s.Body.List {
		index[st.(*ast.CaseClause).Case] = i
	}
	drops := make(map[int]int) // a clause has at most one drop mutant
	type swap struct{ idx, id int }
	var swaps []swap
	for i, c := range cands {
		idx := index[c.Pos]
		if strings.HasPrefix(c.Mutant, switchSwapMutant) {
			swaps = append(swaps, swap{idx, mutIDs[i]})
		} else {
			drops[idx] = mutIDs[i]
		}
	}

	const tagVar = "__kanlyTag"
	var body []ast.Stmt
	if s.Tag != nil {
		body = append(body, &ast.AssignStmt{
			Lhs: []ast.Expr{ast.NewIdent(tagVar)},
			Tok: token.DEFINE,
			Rhs: []ast.Expr{s.Tag},
		})
	}
	defaultIdx := -1
	newClauses := make([]ast.Stmt, len(s.Body.List))
	for i, st := range s.Body.List {
		c := st.(*ast.CaseClause)
		nc := *c
		newClauses[i] = &nc
		if c.List == nil {
			defaultIdx = i
			continue
		}
		nc.List = []ast.Expr{intLit(i)}
		var tests []ast.Stmt
		for _, e := range c.List {
			cond := e
			if s.Tag != nil {
				// Positioned like e, so the printer keeps it on e's line.
				cond = &ast.BinaryExpr{X: &ast.Ident{NamePos: e.Pos(), Name: tagVar}, OpPos: e.Pos(), Op: token.EQL, Y: e}
			}
			tests = append(tests, &ast.IfStmt{
				Cond: cond,
				Body: &ast.BlockStmt{List: []ast.Stmt{&ast.ReturnStmt{Results: []ast.Expr{intLit(i)}}}},
			})
		}
		if id, ok := drops[i]; ok {
			tests = []ast.Stmt{&ast.IfStmt{
				Cond: idCall("__cMutKeep", nil, []int{id}),
				Body: &ast.BlockStmt{List: tests},
			}}
		}
		body = append(body, tests...)
	}
	body = append(body, &ast.ReturnStmt{Results: []ast.Expr{intLit(defaultIdx)}})

	var tag ast.Expr = &ast.CallExpr{Fun: &ast.FuncLit{
		Type: &ast.FuncType{
			Params:  &ast.FieldList{},
			Results: &ast.FieldList{List: []*ast.Field{{Type: ast.NewIdent("int")}}},
		},
		Body: &ast.BlockStmt{List: body},
	}}
	for _, sw := range swaps {
		tag = idCall("__cMutSwap", []ast.Expr{tag, intLit(sw.idx), intLit(sw.idx + 1)}, []int{sw.id})
	}

	ns, block := *s, *s.Body
	ns.Tag, block.List = tag, newClauses
	ns.Body = &block
	return &ns
}

func intLit(n int) ast.Expr {
	return &ast.BasicLit{Kind: token.INT, Value: strconv.Itoa(n)}
}

// idCall builds fn(args..., mutIDs...).
func idCall(fn string, args []ast.Expr, mutIDs []int) *ast.CallExpr {
	for _, id := range mutIDs {
		args = append(args, intLit(id))
	}
	return &ast.CallExpr{Fun: ast.NewIdent(fn), Args: args}
}
