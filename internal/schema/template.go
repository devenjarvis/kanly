package schema

const dispatcherSrc = `package {{.PkgName}}

import (
	"os"
	"strconv"
)

const (
	__cAdd = 1
	__cSub = 2
	__cMul = 3
	__cQuo = 4
	__cRem = 5
	__cLT  = 11
	__cLE  = 12
	__cGT  = 13
	__cGE  = 14
	__cEQ  = 15
	__cNE  = 16
	__cAnd = 21
	__cOr  = 22

	__cAnd2   = 31
	__cOr2    = 32
	__cXor    = 33
	__cShl    = 34
	__cShr    = 35
	__cAndNot = 36
)

var __kanlyActiveMutant int

func init() {
	s := os.Getenv("KANLY_MUTANT")
	if s == "" {
		__kanlyActiveMutant = -1
		return
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < 1 {
		__kanlyActiveMutant = -1
		return
	}
	__kanlyActiveMutant = v
}

// __cMutInt executes either the mutant or the original integer binary operation.
// op encodes the original operation; mutIDs lists all mutation IDs active at this call site.
func __cMutInt(a, b int, op int, mutIDs ...int) int {
	for _, id := range mutIDs {
		if id == __kanlyActiveMutant {
			switch id {
{{- range .IntArithMuts}}
			case {{.ID}}: return {{mutantExpr .}}
{{- end}}
			}
		}
	}
	switch op {
	case __cAdd:
		return a + b
	case __cSub:
		return a - b
	case __cMul:
		return a * b
	case __cQuo:
		return a / b
	case __cRem:
		return a % b
	}
	panic("__cMutInt: unknown op")
}
{{if .IntCmpMuts}}
// __cMutIntCmp executes either the mutant or the original integer comparison.
// op encodes the original operation; mutIDs lists all mutation IDs active at this call site.
func __cMutIntCmp(a, b int, op int, mutIDs ...int) bool {
	for _, id := range mutIDs {
		if id == __kanlyActiveMutant {
			switch id {
{{- range .IntCmpMuts}}
			case {{.ID}}: return {{intCmpExpr .}}
{{- end}}
			}
		}
	}
	switch op {
	case __cLT:
		return a < b
	case __cLE:
		return a <= b
	case __cGT:
		return a > b
	case __cGE:
		return a >= b
	case __cEQ:
		return a == b
	case __cNE:
		return a != b
	}
	panic("__cMutIntCmp: unknown op")
}
{{end}}{{if .BoolLogicMuts}}
// __cMutBool executes either the mutant or the original boolean binary operation.
// Operands are passed as closures to preserve short-circuit semantics.
// op encodes the original operation; mutIDs lists all mutation IDs active at this call site.
func __cMutBool(a, b func() bool, op int, mutIDs ...int) bool {
	for _, id := range mutIDs {
		if id == __kanlyActiveMutant {
			switch id {
{{- range .BoolLogicMuts}}
			case {{.ID}}: return {{boolLogicExpr .}}
{{- end}}
			}
		}
	}
	switch op {
	case __cAnd:
		return a() && b()
	case __cOr:
		return a() || b()
	}
	panic("__cMutBool: unknown op")
}
{{end}}{{if .BoolNotMuts}}
// __cMutNot executes either the mutant or the original boolean negation.
func __cMutNot(x bool, mutIDs ...int) bool {
	for _, id := range mutIDs {
		if id == __kanlyActiveMutant {
			switch id {
{{- range .BoolNotMuts}}
			case {{.ID}}: return x
{{- end}}
			}
		}
	}
	return !x
}
{{end}}{{if .ErrReturnMuts}}
// __cMutErr returns nil for an active mutant, or the original error otherwise.
func __cMutErr(x error, mutIDs ...int) error {
	for _, id := range mutIDs {
		if id == __kanlyActiveMutant {
			switch id {
{{- range .ErrReturnMuts}}
			case {{.ID}}: return nil
{{- end}}
			}
		}
	}
	return x
}
{{end}}{{if .CallDeleteMuts}}
// __cMutCallSkip invokes fn unless one of mutIDs is the active mutant, in which
// case it does nothing — neither the call nor its arguments are evaluated.
func __cMutCallSkip(fn func(), mutIDs ...int) {
	for _, id := range mutIDs {
		if id == __kanlyActiveMutant {
			return
		}
	}
	fn()
}
{{end}}{{if .StringLitMuts}}
// __cMutString returns "" for an active mutant, or orig otherwise.
func __cMutString(orig string, mutIDs ...int) string {
	for _, id := range mutIDs {
		if id == __kanlyActiveMutant {
			return ""
		}
	}
	return orig
}
{{end}}{{if .SliceIdxMuts}}
// __cMutIdx returns i offset by +1 or -1 when one of mutIDs is active, otherwise i.
func __cMutIdx(i int, mutIDs ...int) int {
	for _, id := range mutIDs {
		if id == __kanlyActiveMutant {
			switch id {
{{- range .SliceIdxMuts}}
			case {{.ID}}: return {{sliceIdxExpr .}}
{{- end}}
			}
		}
	}
	return i
}
{{end}}{{if .StmtSwapMuts}}
// __cMutStmt runs mut() if any of mutIDs is the active mutant, otherwise orig().
// Used to swap a whole statement (e.g. x++ ↔ x--, x += y ↔ x -= y) without
// re-evaluating the LHS — both branches are pre-built closures.
func __cMutStmt(orig, mut func(), mutIDs ...int) {
	for _, id := range mutIDs {
		if id == __kanlyActiveMutant {
			mut()
			return
		}
	}
	orig()
}
{{end}}{{if .IntBitMuts}}
// __cMutIntBit executes either the mutant or the original integer bitwise/shift
// binary operation. op encodes the original operation; mutIDs lists all
// mutation IDs active at this call site.
func __cMutIntBit(a, b int, op int, mutIDs ...int) int {
	for _, id := range mutIDs {
		if id == __kanlyActiveMutant {
			switch id {
{{- range .IntBitMuts}}
			case {{.ID}}: return {{intBitExpr .}}
{{- end}}
			}
		}
	}
	switch op {
	case __cAnd2:
		return a & b
	case __cOr2:
		return a | b
	case __cXor:
		return a ^ b
	case __cShl:
		return a << b
	case __cShr:
		return a >> b
	case __cAndNot:
		return a &^ b
	}
	panic("__cMutIntBit: unknown op")
}
{{end}}{{if .IntLitMuts}}
// __cMutIntLit returns the active mutant's int literal value if any of mutIDs
// is the active mutant, otherwise orig.
func __cMutIntLit(orig int, mutIDs ...int) int {
	for _, id := range mutIDs {
		if id == __kanlyActiveMutant {
			switch id {
{{- range .IntLitMuts}}
			case {{.ID}}: return {{.Mutant}}
{{- end}}
			}
		}
	}
	return orig
}
{{end}}{{if .RetZeroMuts}}
// __cMutRetZero returns the zero value of T for an active mutant, or orig
// otherwise. T is inferred at each call site from the return expression's
// static type. var-zero gives 0 / "" / false / nil per T's kind.
func __cMutRetZero[T any](orig T, mutIDs ...int) T {
	for _, id := range mutIDs {
		if id == __kanlyActiveMutant {
			var zero T
			return zero
		}
	}
	return orig
}
{{end}}`
