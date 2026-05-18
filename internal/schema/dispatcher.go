package schema

import (
	"bytes"
	"text/template"

	"github.com/devenjarvis/kanly/internal/mutation"
)

func boolLogicExpr(m mutation.Mutation) string {
	switch m.Mutant {
	case "&&":
		return "a() && b()"
	}
	return "a() || b()"
}

func mutantExpr(m mutation.Mutation) string {
	switch m.Mutant {
	case "+":
		return "a + b"
	case "-":
		return "a - b"
	case "*":
		return "a * b"
	case "/":
		return "a / b"
	case "%":
		return "a % b"
	}
	return "a + b"
}

func sliceIdxExpr(m mutation.Mutation) string {
	switch m.Mutant {
	case "+1":
		return "i + 1"
	case "-1":
		return "i - 1"
	}
	return "i"
}

func intBitExpr(m mutation.Mutation) string {
	switch m.Mutant {
	case "&":
		return "a & b"
	case "|":
		return "a | b"
	case "^":
		return "a ^ b"
	case "<<":
		return "a << b"
	case ">>":
		return "a >> b"
	case "&^":
		return "a &^ b"
	}
	return "a & b"
}

func intCmpExpr(m mutation.Mutation) string {
	switch m.Mutant {
	case "<":
		return "a < b"
	case "<=":
		return "a <= b"
	case ">":
		return "a > b"
	case ">=":
		return "a >= b"
	case "==":
		return "a == b"
	case "!=":
		return "a != b"
	}
	return "a < b"
}

func byOperator(muts []mutation.Mutation, names ...string) []mutation.Mutation {
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	var result []mutation.Mutation
	for _, m := range muts {
		if set[m.OperatorName] {
			result = append(result, m)
		}
	}
	return result
}

// RenderDispatcher renders the schema dispatcher source for the given package and mutations.
func RenderDispatcher(pkgName string, muts []mutation.Mutation) (string, error) {
	tmpl, err := template.New("dispatcher").Funcs(template.FuncMap{
		"mutantExpr":    mutantExpr,
		"intCmpExpr":    intCmpExpr,
		"boolLogicExpr": boolLogicExpr,
		"sliceIdxExpr":  sliceIdxExpr,
		"intBitExpr":    intBitExpr,
	}).Parse(dispatcherSrc)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, map[string]interface{}{
		"PkgName":       pkgName,
		"IntArithMuts":  byOperator(muts, "int_arith"),
		"IntCmpMuts":    byOperator(muts, "int_cmp_boundary", "int_cmp_negate"),
		"BoolLogicMuts": byOperator(muts, "bool_logic"),
		"BoolNotMuts":   byOperator(muts, "bool_not"),
		"ErrReturnMuts":  byOperator(muts, "err_return_nil"),
		"CallDeleteMuts": byOperator(muts, "call_delete"),
		"StringLitMuts":  byOperator(muts, "string_literal"),
		"SliceIdxMuts":   byOperator(muts, "slice_index"),
		"StmtSwapMuts":   byOperator(muts, "inc_dec", "int_compound_assign"),
		"IntBitMuts":     byOperator(muts, "int_bitwise"),
		"IntLitMuts":     byOperator(muts, "int_literal"),
		"RetZeroMuts":    byOperator(muts, "return_zero"),
	}); err != nil {
		return "", err
	}
	return buf.String(), nil
}
