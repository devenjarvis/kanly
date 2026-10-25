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

// __cInteger constrains the integer-operand dispatchers to any basic integer
// type or a defined type whose underlying type is a basic integer.
type __cInteger interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr
}

// __cFloat constrains the float-operand dispatchers to float32, float64 or a
// defined type whose underlying type is one of them.
type __cFloat interface {
	~float32 | ~float64
}

// __cBoolean constrains the bool-operand dispatchers to plain bool or any
// defined type whose underlying type is bool.
type __cBoolean interface {
	~bool
}

// __kanlyActiveMutant is set by a variable initializer, not an init(): Go
// initializes package variables in dependency order before running any
// init(), so it is ready for every mutated var initializer and init() body
// that reaches a dispatcher, whatever file they live in.
var __kanlyActiveMutant = __kanlyReadActiveMutant()

func __kanlyReadActiveMutant() int {
	v, err := strconv.Atoi(os.Getenv("KANLY_MUTANT"))
	if err != nil || v < 1 {
		return -1
	}
	return v
}

// __cMutInt executes either the mutant or the original integer binary operation.
// op encodes the original operation; mutIDs lists all mutation IDs active at this call site.
func __cMutInt[T __cInteger](a, b T, op int, mutIDs ...int) T {
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
{{if .FloatArithMuts}}
// __cMutFloat executes either the mutant or the original float binary operation.
// op encodes the original operation; mutIDs lists all mutation IDs active at this call site.
func __cMutFloat[T __cFloat](a, b T, op int, mutIDs ...int) T {
	for _, id := range mutIDs {
		if id == __kanlyActiveMutant {
			switch id {
{{- range .FloatArithMuts}}
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
	}
	panic("__cMutFloat: unknown op")
}
{{end}}{{if .FloatCmpMuts}}
// __cMutFloatCmp executes either the mutant or the original float comparison.
// op encodes the original operation; mutIDs lists all mutation IDs active at this call site.
func __cMutFloatCmp[T __cFloat](a, b T, op int, mutIDs ...int) bool {
	for _, id := range mutIDs {
		if id == __kanlyActiveMutant {
			switch id {
{{- range .FloatCmpMuts}}
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
	panic("__cMutFloatCmp: unknown op")
}
{{end}}{{if .IntCmpMuts}}
// __cMutIntCmp executes either the mutant or the original integer comparison.
// op encodes the original operation; mutIDs lists all mutation IDs active at this call site.
func __cMutIntCmp[T __cInteger](a, b T, op int, mutIDs ...int) bool {
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
// a is the already-evaluated left operand (Go evaluates it first under both
// && and ||) and fixes T, so named bools need no type name at the call site.
// b is a closure so the right operand keeps its short-circuit semantics.
// op encodes the original operation; mutIDs lists all mutation IDs active at this call site.
func __cMutBool[T __cBoolean](a T, b func() bool, op int, mutIDs ...int) T {
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
		return T(bool(a) && b())
	case __cOr:
		return T(bool(a) || b())
	}
	panic("__cMutBool: unknown op")
}
{{end}}{{if .BoolNotMuts}}
// __cMutNot executes either the mutant or the original boolean negation.
func __cMutNot[T __cBoolean](x T, mutIDs ...int) T {
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
{{end}}{{if .ChanOpMuts}}
// __cMutRecv receives from ch, or returns the zero value of its element type
// without touching ch when one of mutIDs is the active mutant. A
// bidirectional or named channel type unifies with <-chan T.
func __cMutRecv[T any](ch <-chan T, mutIDs ...int) T {
	for _, id := range mutIDs {
		if id == __kanlyActiveMutant {
			var zero T
			return zero
		}
	}
	return <-ch
}
{{end}}{{if .SwitchCaseMuts}}
// __cMutKeep reports false when one of mutIDs is the active mutant: the
// switch clause it guards is dropped.
func __cMutKeep(mutIDs ...int) bool {
	for _, id := range mutIDs {
		if id == __kanlyActiveMutant {
			return false
		}
	}
	return true
}

// __cMutSwap exchanges the clause indices a and b of a rewritten switch when
// one of mutIDs is the active mutant, so each clause runs the other's body.
func __cMutSwap(idx, a, b int, mutIDs ...int) int {
	for _, id := range mutIDs {
		if id == __kanlyActiveMutant {
			switch idx {
			case a:
				return b
			case b:
				return a
			}
		}
	}
	return idx
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
func __cMutIdx[T __cInteger](i T, mutIDs ...int) T {
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
func __cMutIntBit[T __cInteger](a, b T, op int, mutIDs ...int) T {
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

// __cMutLitOff returns 1 when one of mutIDs is the active mutant, 0 otherwise.
// A sized or named integer literal n becomes n*(1>>__cMutLitOff(ids...)) plus
// one v*(1>>__cMutLitOn(id)) per mutant value v: an untyped constant shifted
// by a non-constant amount takes its type from the context, so no type name
// is needed at the call site.
func __cMutLitOff(mutIDs ...int) uint {
	for _, id := range mutIDs {
		if id == __kanlyActiveMutant {
			return 1
		}
	}
	return 0
}

// __cMutLitOn returns 0 when id is the active mutant, 1 otherwise; see
// __cMutLitOff.
func __cMutLitOn(id int) uint {
	if id == __kanlyActiveMutant {
		return 0
	}
	return 1
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
{{end}}{{if .BoolLitMuts}}
// __cMutBoolLit returns the negation of orig for an active mutant, or orig
// otherwise — used to flip predeclared true/false literals at their use site.
func __cMutBoolLit(orig bool, mutIDs ...int) bool {
	for _, id := range mutIDs {
		if id == __kanlyActiveMutant {
			return !orig
		}
	}
	return orig
}
{{end}}`
