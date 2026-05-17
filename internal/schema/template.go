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
{{end}}`
