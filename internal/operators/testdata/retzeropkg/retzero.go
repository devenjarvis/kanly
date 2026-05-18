package retzeropkg

import "io"

// IntIdent — return of bare ident, int. Expected: 1 candidate (→0).
func IntIdent(x int) int { return x }

// StringIdent — return of bare ident, string. Expected: 1 candidate (→"").
func StringIdent(s string) string { return s }

// BoolIdent — return of bare ident, bool. Expected: 1 candidate (→false).
func BoolIdent(b bool) bool { return b }

// PtrIdent — return of *T. Expected: 1 candidate (→nil).
func PtrIdent(p *int) *int { return p }

// SliceIdent — return of []T. Expected: 1 candidate (→nil).
func SliceIdent(s []byte) []byte { return s }

// MapIdent — return of map[K]V. Expected: 1 candidate (→nil).
func MapIdent(m map[string]int) map[string]int { return m }

// ChanIdent — return of chan T. Expected: 1 candidate (→nil).
func ChanIdent(c chan int) chan int { return c }

// FuncIdent — return of func type. Expected: 1 candidate (→nil).
func FuncIdent(f func()) func() { return f }

// InterfaceIdent — non-error interface return. Expected: 1 candidate (→nil).
func InterfaceIdent(r io.Reader) io.Reader { return r }

// CallExprReturn — single-value call result. Expected: 1 candidate.
func CallExprReturn() int { return helper() }

func helper() int { return 7 }

// IndexReturn — IndexExpr at return position. Expected: 1 candidate.
func IndexReturn(a []int) int { return a[0] }

// SelectorReturn — SelectorExpr at return position. Expected: 1 candidate.
type holder struct{ x int }

func (h holder) Get() int { return h.x }

// CompositeLitReturn — composite literal. Expected: 1 candidate (slice→nil).
func CompositeLitReturn() []int { return []int{1, 2, 3} }

// === Skipped cases ===

// BinaryExprReturn — int_arith already targets BinaryExpr. Expected: 0 candidates from return_zero.
func BinaryExprReturn(a, b int) int { return a + b }

// UnaryExprReturn — unary negation. Expected: 0 candidates from return_zero.
func UnaryExprReturn(b bool) bool { return !b }

// LiteralReturn — BasicLit handled by int_literal / string_literal. Expected: 0 from return_zero.
func LiteralReturn() int { return 42 }

// LiteralNilReturn — literal nil, no-op. Expected: 0.
func LiteralNilReturn() *int { return nil }

// LiteralFalseReturn — literal false, no-op. Expected: 0.
func LiteralFalseReturn() bool { return false }

// ErrorReturn — handled by err_return_nil. Expected: 0 from return_zero.
func ErrorReturn(err error) error { return err }

// NamedIntReturn — strict-identity excludes named int. Expected: 0.
type MyInt int

func NamedIntReturn(x MyInt) MyInt { return x }

// Float64Return — only int is the supported numeric kind. Expected: 0.
func Float64Return(x float64) float64 { return x }

// NakedReturn — no Results, nothing to wrap. Expected: 0.
func NakedReturn() (n int) {
	n = 5
	return
}

// MultiValueCallPassthrough — wrapping a multi-value call breaks Go. Expected: 0.
func multiResult() (int, string) { return 1, "ok" }
func MultiValueCallPassthrough() (int, string) {
	return multiResult()
}

// MixedReturn — int and string in same return. Expected: 2 candidates (both idents).
func MixedReturn(i int, s string) (int, string) { return i, s }
