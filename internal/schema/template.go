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
// op encodes the original operation; mutID identifies which mutant this call site belongs to.
func __cMutInt(a, b int, op int, mutID int) int {
	if mutID == __cauldronActiveMutant {
		switch mutID {
{{- range .Muts}}
		case {{.ID}}: return {{mutantExpr .}}
{{- end}}
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
	}
	panic("__cMutInt: unknown op")
}
`
