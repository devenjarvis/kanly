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
)

var __cauldronActiveMutant int

func init() {
	s := os.Getenv("CAULDRON_MUTANT")
	if s == "" {
		__cauldronActiveMutant = -1
		return
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < 1 {
		__cauldronActiveMutant = -1
		return
	}
	__cauldronActiveMutant = v
}

// __cMutInt executes either the mutant or the original integer binary operation.
// op encodes the original operation; mutIDs lists all mutation IDs active at this call site.
func __cMutInt(a, b int, op int, mutIDs ...int) int {
	for _, id := range mutIDs {
		if id == __cauldronActiveMutant {
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
		if id == __cauldronActiveMutant {
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
{{end}}`
