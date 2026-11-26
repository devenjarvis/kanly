package schema

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"testing"
)

func TestIsZeroLiteral(t *testing.T) {
	const src = `package p

var v int

func f() {
	_ = []any{0, 0x0, 0.0, 1, 1.5, "", "x", ` + "``" + `, false, true, nil, v, -0, int(0)}
}

func g() {
	false := 0
	_ = false
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Uses: map[*ast.Ident]types.Object{}}
	if _, err := (&types.Config{Importer: importer.Default()}).Check("p", fset, []*ast.File{file}, info); err != nil {
		t.Fatal(err)
	}
	var elts []ast.Expr
	var shadowed ast.Expr
	ast.Inspect(file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CompositeLit:
			elts = x.Elts
		case *ast.AssignStmt:
			if x.Tok == token.ASSIGN {
				shadowed = x.Rhs[0]
			}
		}
		return true
	})
	want := []bool{true, true, true, false, false, true, false, true, true, false, true, false, false, false}
	if len(elts) != len(want) {
		t.Fatalf("found %d elements, want %d", len(elts), len(want))
	}
	for i, e := range elts {
		if got := isZeroLiteral(info, e); got != want[i] {
			t.Errorf("isZeroLiteral(%s) = %v, want %v", types.ExprString(e), got, want[i])
		}
	}
	if isZeroLiteral(info, shadowed) {
		t.Error("a local variable named false is not the predeclared false")
	}
}
