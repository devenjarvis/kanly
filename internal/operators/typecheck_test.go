package operators

import (
	"go/ast"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/devenjarvis/cauldron/internal/source"
)

func TestBoolOperandsTypeCheck(t *testing.T) {
	tests := []struct {
		name      string
		pkgPath   string
		wantTrue  int
		wantFalse int
	}{
		// boolpkg: And() b1&&b2 (true), Or() b1||b2 (true), ConstAnd() true&&false (false, untyped)
		{"boolpkg bool vars", relDirTC(t, "testdata/boolpkg"), 2, 1},
		// namedboolpkg: MyAnd() m1&&m2 (false, named MyBool type)
		{"namedboolpkg named bool", relDirTC(t, "testdata/namedboolpkg"), 0, 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pkg, err := source.Load(tc.pkgPath)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}

			var trueCount, falseCount int
			for _, f := range pkg.Files {
				ast.Inspect(f, func(n ast.Node) bool {
					expr, ok := n.(*ast.BinaryExpr)
					if !ok {
						return true
					}
					if boolOperands(pkg.TypesInfo, expr.X, expr.Y) {
						trueCount++
					} else {
						falseCount++
					}
					return true
				})
			}

			if trueCount != tc.wantTrue {
				t.Errorf("boolOperands true count: want %d, got %d", tc.wantTrue, trueCount)
			}
			if falseCount != tc.wantFalse {
				t.Errorf("boolOperands false count: want %d, got %d", tc.wantFalse, falseCount)
			}
		})
	}
}

func TestBoolOperandTypeCheck(t *testing.T) {
	tests := []struct {
		name      string
		pkgPath   string
		wantTrue  int
		wantFalse int
	}{
		// boolpkg: Not() !b1 (true), ConstNot() !true (false, untyped bool constant)
		{"boolpkg bool var", relDirTC(t, "testdata/boolpkg"), 1, 1},
		// namedboolpkg: MyNot() !m1 (false, named MyBool type)
		{"namedboolpkg named bool", relDirTC(t, "testdata/namedboolpkg"), 0, 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pkg, err := source.Load(tc.pkgPath)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}

			var trueCount, falseCount int
			for _, f := range pkg.Files {
				ast.Inspect(f, func(n ast.Node) bool {
					expr, ok := n.(*ast.UnaryExpr)
					if !ok {
						return true
					}
					if boolOperand(pkg.TypesInfo, expr.X) {
						trueCount++
					} else {
						falseCount++
					}
					return true
				})
			}

			if trueCount != tc.wantTrue {
				t.Errorf("boolOperand true count: want %d, got %d", tc.wantTrue, trueCount)
			}
			if falseCount != tc.wantFalse {
				t.Errorf("boolOperand false count: want %d, got %d", tc.wantFalse, falseCount)
			}
		})
	}
}

func relDirTC(t *testing.T, sub string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	abs, err := filepath.Abs(filepath.Join(filepath.Dir(file), sub))
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func TestIntOperandsTypeCheck(t *testing.T) {
	tests := []struct {
		name      string
		pkgPath   string
		wantTrue  int
		wantFalse int
	}{
		// simple: func Add(a, b int) int { return a + b } → one true
		{"simple int+int", relDirTC(t, "../source/testdata/simple"), 1, 0},
		// floatpkg: func Add(a, b float64) float64 { return a + b } → one false
		{"floatpkg float64+float64", relDirTC(t, "testdata/floatpkg"), 0, 1},
		// constpkg: const x = 1+2 (false) and func Add(a,b int) → one true + one false
		{"constpkg const+func", relDirTC(t, "testdata/constpkg"), 1, 1},
		// namedintpkg: type MyInt int; func Add(a, b MyInt) MyInt { return a+b } → one false
		{"namedintpkg MyInt+MyInt", relDirTC(t, "testdata/namedintpkg"), 0, 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pkg, err := source.Load(tc.pkgPath)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}

			var trueCount, falseCount int
			for _, f := range pkg.Files {
				ast.Inspect(f, func(n ast.Node) bool {
					expr, ok := n.(*ast.BinaryExpr)
					if !ok {
						return true
					}
					if intOperands(pkg.TypesInfo, expr.X, expr.Y) {
						trueCount++
					} else {
						falseCount++
					}
					return true
				})
			}

			if trueCount != tc.wantTrue {
				t.Errorf("intOperands true count: want %d, got %d", tc.wantTrue, trueCount)
			}
			if falseCount != tc.wantFalse {
				t.Errorf("intOperands false count: want %d, got %d", tc.wantFalse, falseCount)
			}
		})
	}
}
