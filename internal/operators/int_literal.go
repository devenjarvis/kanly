package operators

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"math"
	"strconv"

	"github.com/devenjarvis/kanly/internal/mutation"
)

// IntLiteral mutates every integer basic literal to a small set of canonical
// alternates: 0, 1, -n, n+1, n-1 (skipping no-ops where the mutant value equals
// the original, and values the literal's type cannot represent, such as -n
// for an unsigned type). Magic-number bugs and off-by-one errors live in
// literals; weak tests that don't pin exact values won't kill these mutants.
//
// Skipped positions (would not compile or are not user-meaningful):
//   - Import specs (no int literals there, but defensive).
//   - Const declarations — initializers must be constant expressions.
//   - Struct field tags (string-only, but defensive).
//   - Array type lengths (e.g. `[10]int{}`) — must be a constant expression.
//   - Array/slice composite-literal keys (e.g. `[]string{0: "a"}`) — likewise.
//   - Literals that stay untyped (a constant shift count) or take a
//     non-integer type (`var f float64 = 5`).
//   - Literals inside a constant expression whose value would change if it
//     were evaluated at run time; see inWrapSensitiveConstant.
//
// A literal whose contextual type is plain int is wrapped in
// __cMutIntLit(n, mutIDs...). Any other integer type — sized (`int64`),
// named (`type ID int32`, `time.Duration`) — cannot be spelled at the call
// site, and a generic dispatcher would infer int from the untyped literal,
// so the literal becomes a sum that takes its type from the context:
//
//	n*(1>>__cMutLitOff(ids...)) + v1*(1>>__cMutLitOn(id1)) + ...
//
// An untyped constant shifted by a non-constant amount takes the type its
// context gives the constant alone, and exactly one term is non-zero.
type IntLiteral struct{}

func (IntLiteral) Name() string          { return "int_literal" }
func (IntLiteral) DispatcherKey() string { return "int_literal" }

// Rewrite returns __cMutIntLit(<orig BasicLit>, mutIDs...) for a plain int
// literal; see RewriteGroup.
func (op IntLiteral) Rewrite(c mutation.Candidate, mutIDs []int) ast.Node {
	return op.RewriteGroup([]mutation.Candidate{c}, mutIDs)
}

// RewriteGroup returns __cMutIntLit(<orig BasicLit>, mutIDs...) for a plain
// int literal, and for any other integer type the context-typed sum that
// spells each candidate's mutant value (see IntLiteral).
func (IntLiteral) RewriteGroup(cands []mutation.Candidate, mutIDs []int) ast.Node {
	lit := cands[0].Node.(*ast.BasicLit)
	if t := cands[0].Type; t == nil || t == types.Typ[types.Int] {
		args := []ast.Expr{lit}
		for _, id := range mutIDs {
			args = append(args, &ast.BasicLit{Kind: token.INT, Value: fmt.Sprintf("%d", id)})
		}
		return &ast.CallExpr{Fun: &ast.Ident{Name: "__cMutIntLit"}, Args: args}
	}
	var sum ast.Expr
	add := func(value ast.Expr, sel *ast.CallExpr) {
		term := &ast.BinaryExpr{
			X:  value,
			Op: token.MUL,
			Y:  &ast.ParenExpr{X: &ast.BinaryExpr{X: intLit(1), Op: token.SHR, Y: sel}},
		}
		if sum == nil {
			sum = term
			return
		}
		sum = &ast.BinaryExpr{X: sum, Op: token.ADD, Y: term}
	}
	// A copy without a position: a positioned token ahead of the unpositioned
	// rest makes the printer flush comments into the middle of the sum.
	add(&ast.BasicLit{Kind: token.INT, Value: lit.Value}, idCall("__cMutLitOff", nil, mutIDs))
	for i, c := range cands {
		if c.Mutant == "0" {
			continue // a zero term adds nothing
		}
		add(&ast.BasicLit{Kind: token.INT, Value: c.Mutant}, idCall("__cMutLitOn", nil, mutIDs[i:i+1]))
	}
	return &ast.ParenExpr{X: sum}
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
			if t := intLitType(info, lit); t != nil && litMutable(stack) && !inConstantPosition(info, stack, lit) && !inWrapSensitiveConstant(info, stack, lit) {
				for _, m := range intLitMutants(lit.Value, t) {
					candidates = append(candidates, mutation.Candidate{
						Node:     lit,
						Pos:      lit.Pos(),
						Original: lit.Value,
						Mutant:   m,
						Type:     t,
					})
				}
			}
		}
		stack = append(stack, n)
		return true
	})
	return candidates
}

// intLitType returns lit's contextual type when it is a typed integer type,
// or nil. Unlike intOperand it ACCEPTS compile-time constants — every int
// literal is one.
func intLitType(info *types.Info, lit *ast.BasicLit) types.Type {
	tv, ok := info.Types[lit]
	if !ok || !isIntegerType(tv.Type) {
		return nil
	}
	if b, ok := tv.Type.Underlying().(*types.Basic); ok && b.Info()&types.IsUntyped != 0 {
		return nil
	}
	return tv.Type
}

// litMutable reports whether a literal (int or bool keyword) can be safely
// replaced with a function call at its current position. Rejects const-decl
// initializers (must be const exprs), array type lengths (must be const),
// and the rare import-spec position (defensive — neither int nor bool
// literals normally appear there).
func litMutable(stack []ast.Node) bool {
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

// intLitMutants returns the deduplicated mutant int values for orig (as Go
// source) that t can represent. orig is in any int-literal form recognized by
// strconv.ParseInt with base 0: decimal, 0x.., 0o.., 0b.., underscore-separated.
// The output is always decimal.
func intLitMutants(orig string, t types.Type) []string {
	n, err := strconv.ParseInt(orig, 0, 64)
	if err != nil {
		return nil
	}
	lo, hi := intRange(t, n)
	if n > hi {
		// 128 in `var x int8 = -128`: only the negation is representable.
		return nil
	}
	seen := map[int64]bool{n: true}
	var out []string
	consider := func(v int64) {
		if seen[v] || v < lo || v > hi {
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

// intRange returns the bounds of the integer type t, clamped to int64. The
// width of int, uint and uintptr depends on the target, so they are taken as
// 32 bits unless n, a value they hold, needs 64.
func intRange(t types.Type, n int64) (lo, hi int64) {
	b := t.Underlying().(*types.Basic)
	bits := 64
	switch b.Kind() {
	case types.Int8, types.Uint8:
		bits = 8
	case types.Int16, types.Uint16:
		bits = 16
	case types.Int32, types.Uint32:
		bits = 32
	case types.Int:
		if n >= math.MinInt32 && n <= math.MaxInt32 {
			bits = 32
		}
	case types.Uint, types.Uintptr:
		if n >= 0 && n <= math.MaxUint32 {
			bits = 32
		}
	}
	if b.Info()&types.IsUnsigned != 0 {
		if bits == 64 {
			return 0, math.MaxInt64
		}
		return 0, 1<<bits - 1
	}
	return -1 << (bits - 1), 1<<(bits-1) - 1
}

// inWrapSensitiveConstant reports whether lit is an operand of a constant
// expression that holds a division, remainder, right shift or comparison.
// Rewriting the literal makes the expression non-constant, so its
// intermediate results wrap at the type's width: harmless for + - * << and
// the bitwise operators, which agree with the exact result modulo 2^n, but
// `int32(1 << 40 >> 20)` would change value with no mutant active.
func inWrapSensitiveConstant(info *types.Info, stack []ast.Node, lit *ast.BasicLit) bool {
	// stack[0] is the file, so the walk always stops.
	for i := len(stack) - 1; ; i-- {
		e, ok := stack[i].(ast.Expr)
		if !ok {
			return false
		}
		if tv, ok := info.Types[e]; !ok || tv.Value == nil {
			return false
		}
		if b, ok := e.(*ast.BinaryExpr); ok {
			switch b.Op {
			case token.QUO, token.REM, token.SHR,
				token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
				return true
			}
		}
	}
}
