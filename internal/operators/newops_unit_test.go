package operators

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/printer"
	"go/token"
	"go/types"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/devenjarvis/kanly/internal/mutation"
)

// typecheck parses and type-checks src, a file of package p that imports
// only the standard library.
func typecheck(t *testing.T, src string) (*ast.File, *types.Info) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{
		Types: map[ast.Expr]types.TypeAndValue{},
		Defs:  map[*ast.Ident]types.Object{},
		Uses:  map[*ast.Ident]types.Object{},
	}
	if _, err := (&types.Config{Importer: importer.Default()}).Check("p", fset, []*ast.File{f}, info); err != nil {
		t.Fatal(err)
	}
	return f, info
}

func printNode(t *testing.T, n ast.Node) string {
	t.Helper()
	var b strings.Builder
	if err := printer.Fprint(&b, token.NewFileSet(), n); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestIntRange(t *testing.T) {
	named := types.NewNamed(types.NewTypeName(token.NoPos, nil, "Level", nil), types.Typ[types.Int16], nil)
	for _, tc := range []struct {
		t      types.Type
		n      int64
		lo, hi int64
	}{
		{types.Typ[types.Int8], 1, math.MinInt8, math.MaxInt8},
		{types.Typ[types.Uint8], 1, 0, math.MaxUint8},
		{types.Typ[types.Int16], 1, math.MinInt16, math.MaxInt16},
		{named, 1, math.MinInt16, math.MaxInt16},
		{types.Typ[types.Uint16], 1, 0, math.MaxUint16},
		{types.Typ[types.Int32], 1, math.MinInt32, math.MaxInt32},
		{types.Typ[types.Uint32], 1, 0, math.MaxUint32},
		{types.Typ[types.Int64], 1, math.MinInt64, math.MaxInt64},
		{types.Typ[types.Uint64], 1, 0, math.MaxInt64},
		// int, uint and uintptr are taken as 32 bits unless n needs 64.
		{types.Typ[types.Int], 1, math.MinInt32, math.MaxInt32},
		{types.Typ[types.Int], math.MaxInt32, math.MinInt32, math.MaxInt32},
		{types.Typ[types.Int], math.MaxInt32 + 1, math.MinInt64, math.MaxInt64},
		{types.Typ[types.Int], math.MinInt32, math.MinInt32, math.MaxInt32},
		{types.Typ[types.Int], math.MinInt32 - 1, math.MinInt64, math.MaxInt64},
		{types.Typ[types.Uint], 0, 0, math.MaxUint32},
		{types.Typ[types.Uint], math.MaxUint32, 0, math.MaxUint32},
		{types.Typ[types.Uint], math.MaxUint32 + 1, 0, math.MaxInt64},
		{types.Typ[types.Uintptr], -1, 0, math.MaxInt64},
		{types.Typ[types.Uintptr], 7, 0, math.MaxUint32},
	} {
		if lo, hi := intRange(tc.t, tc.n); lo != tc.lo || hi != tc.hi {
			t.Errorf("intRange(%s, %d) = %d, %d; want %d, %d", tc.t, tc.n, lo, hi, tc.lo, tc.hi)
		}
	}
}

func TestIntLitMutantsFitTheType(t *testing.T) {
	for _, tc := range []struct {
		lit  string
		t    types.Type
		want []string
	}{
		{"42", types.Typ[types.Int], []string{"0", "1", "-42", "43", "41"}},
		{"0x10", types.Typ[types.Int64], []string{"0", "1", "-16", "17", "15"}},
		{"0", types.Typ[types.Uint8], []string{"1"}},
		{"255", types.Typ[types.Uint8], []string{"0", "1", "254"}},
		{"127", types.Typ[types.Int8], []string{"0", "1", "-127", "126"}},
		{"126", types.Typ[types.Int8], []string{"0", "1", "-126", "127", "125"}},
		{"128", types.Typ[types.Int8], nil}, // the operand of -128
		{"99999999999999999999", types.Typ[types.Uint64], nil},
	} {
		if got := intLitMutants(tc.lit, tc.t); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("intLitMutants(%s, %s) = %q, want %q", tc.lit, tc.t, got, tc.want)
		}
	}
}

func TestIntLiteralRewriteGroupSpellsEachValue(t *testing.T) {
	cands := func(value string, typ types.Type, mutants ...string) []mutation.Candidate {
		lit := &ast.BasicLit{ValuePos: 1, Kind: token.INT, Value: value}
		var out []mutation.Candidate
		for _, m := range mutants {
			out = append(out, mutation.Candidate{Node: lit, Original: value, Mutant: m, Type: typ})
		}
		return out
	}
	for _, tc := range []struct {
		name  string
		cands []mutation.Candidate
		ids   []int
		want  string
	}{
		{"plain int", cands("5", types.Typ[types.Int], "0", "6"), []int{3, 4}, "__cMutIntLit(5, 3, 4)"},
		{"untyped", cands("5", nil, "1"), []int{9}, "__cMutIntLit(5, 9)"},
		{"sized", cands("5", types.Typ[types.Int32], "0", "1", "-5"), []int{7, 8, 9},
			"(5*(1>>__cMutLitOff(7, 8, 9)) + 1*(1>>__cMutLitOn(8)) + -5*(1>>__cMutLitOn(9)))"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := printNode(t, IntLiteral{}.RewriteGroup(tc.cands, tc.ids)); got != tc.want {
				t.Errorf("got  %s\nwant %s", got, tc.want)
			}
		})
	}
	// Rewrite is the single-candidate case.
	if got := printNode(t, IntLiteral{}.Rewrite(cands("5", types.Typ[types.Int8], "6")[0], []int{2})); got != "(5*(1>>__cMutLitOff(2)) + 6*(1>>__cMutLitOn(2)))" {
		t.Errorf("Rewrite: got %s", got)
	}
}

func TestIntLiteralSkipsWrapSensitiveConstants(t *testing.T) {
	f, info := typecheck(t, `package p
const c int32 = 8
func f(x int32) []any {
	return []any{
		x + 1, x / 9,
		c + 2, c * 3, c << 4, c & 5, c | 6, c ^ 7, c &^ 13, (c + 14) * 2,
		c / 10, c % 11, c >> 12, (c + 15) / 2,
		c == 17, c != 18, c < 19, c <= 20, c > 21, c >= 22,
	}
}
`)
	got := map[string]bool{}
	for _, c := range (IntLiteral{}).Find(f, info) {
		got[c.Original] = true
	}
	// 4 is an untyped shift count, which no operator rewrites.
	for _, lit := range []string{"1", "9", "2", "3", "5", "6", "7", "13", "14"} {
		if !got[lit] {
			t.Errorf("literal %s not mutated", lit)
		}
	}
	for _, lit := range []string{"10", "11", "12", "15", "17", "18", "19", "20", "21", "22"} {
		if got[lit] {
			t.Errorf("literal %s inside a wrap-sensitive constant mutated", lit)
		}
	}
}

func TestHasOwnType(t *testing.T) {
	f, info := typecheck(t, `package p
type B bool
func f(x int64, b B, p *int, ch chan int, n uint) {
	_ = []any{x, 1, x + 1, 1 + x, 1 + 2, (x), -x, -1, &x, *p, <-ch, x << n, 1 << n, x < 2, !b, b && true, true && b, f, &[]int{}, []int{}}
}`)
	var elts []ast.Expr
	ast.Inspect(f, func(n ast.Node) bool {
		if cl, ok := n.(*ast.CompositeLit); ok && elts == nil {
			elts = cl.Elts
		}
		return true
	})
	want := []bool{true, false, true, true, false, true, true, false, true, true, true, true, false, false, true, true, true, true, true, false}
	if len(elts) != len(want) {
		t.Fatalf("%d elements, want %d", len(elts), len(want))
	}
	for i, e := range elts {
		if got := hasOwnType(info, e); got != want[i] {
			t.Errorf("hasOwnType(%s) = %v, want %v", types.ExprString(e), got, want[i])
		}
	}
}

func TestBinaryOperatorsNeedAnInferableType(t *testing.T) {
	f, info := typecheck(t, `package p
func f(n uint, x int64, y float32) (int64, int, int64, float32, float64, bool) {
	return 1<<n - 1, 1<<n - 1, x - 1, y * 2, 1.5 * float64(n), y < 2
}`)
	var got []string
	for _, op := range []mutation.Operator{IntArith{}, FloatArith{}, FloatCmpNegate{}} {
		for _, c := range op.Find(f, info) {
			got = append(got, op.Name()+":"+c.Original)
		}
	}
	// 1<<n - 1 in an int64 context has no operand that fixes the type.
	want := []string{"int_arith:-", "int_arith:-", "float_arith:*", "float_arith:*", "float_cmp_negate:<"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("candidates %q, want %q", got, want)
	}
}

func TestClauseLabel(t *testing.T) {
	f, _ := typecheck(t, `package p
func f(n, a12345678901234567890123456789012, b1234567890123456789012345678901234 int) {
	switch n {
	case 1, 2:
	case a12345678901234567890123456789012, 3:
	case b1234567890123456789012345678901234:
	default:
	}
}`)
	var got []string
	ast.Inspect(f, func(n ast.Node) bool {
		if c, ok := n.(*ast.CaseClause); ok {
			got = append(got, clauseLabel(c))
		}
		return true
	})
	want := []string{
		"case 1, 2",
		"case a12345678901234567890123456789012,…", // shortened to 40 runes
		"case b1234567890123456789012345678901234", // 40 runes: kept whole
		"default",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("labels %q, want %q", got, want)
	}
	if n := len([]rune(got[1])); n != 40 {
		t.Errorf("long label has %d runes, want 40", n)
	}
}

func TestSwitchCaseFindSkips(t *testing.T) {
	f, info := typecheck(t, `package p
func f(n int) (r int) {
	defer func() {
		switch n {
		case recover().(int):
			r = 1
		case 2:
			r = 2
		}
	}()
	switch {
	}
	switch n {
	case 1:
		r = 1
	case 2:
	}
	switch n {
	case 3:
		r = 3
	case 4:
	default:
	}
	switch n {
	case 5:
	case 6:
		r = 6
	}
	switch recover() {
	case nil:
		switch n {
		case 7:
			r = 7
		}
	}
	switch n {
	case 8:
		switch {
		case recover() != nil:
			switch n {
			case 9:
				r = 9
			}
		}
	default:
		r = 0
	case 10:
	}
	return r
}`)
	var got []string
	for _, c := range (SwitchCase{}).Find(f, info) {
		got = append(got, c.Original+"→"+c.Mutant)
	}
	want := []string{
		// case 2 is empty, last and without a default: dropping it is a no-op.
		"case 1→", "case 1→swap with case 2",
		// with a default, an empty case 4 still matters.
		"case 3→", "case 4→", "case 3→swap with case 4",
		// an empty clause that is not last can be dropped.
		"case 5→", "case 6→", "case 5→swap with case 6",
		// switches nested in skipped ones are still mutated; with a
		// default, an empty last case still matters.
		"case 7→",
		"case 8→", "case 10→", "case 8→swap with default", "default→swap with case 10",
		"case 9→",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("candidates:\n got %q\nwant %q", got, want)
	}
}

func TestSwitchCaseRewriteIsTheGroupOfOne(t *testing.T) {
	f, info := typecheck(t, `package p
func f(n int) int {
	switch n {
	case 1:
		return 1
	}
	return 0
}`)
	c := (SwitchCase{}).Find(f, info)[0]
	got := printNode(t, SwitchCase{}.Rewrite(c, []int{5}).(*ast.SwitchStmt).Tag)
	if !strings.Contains(got, "__cMutKeep(5)") {
		t.Errorf("Rewrite did not guard the dropped clause: %s", got)
	}
}
