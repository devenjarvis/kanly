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
		"mutantExpr":   mutantExpr,
		"intCmpExpr":   intCmpExpr,
		"boolLogicExpr": boolLogicExpr,
	}).Parse(dispatcherSrc)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, map[string]interface{}{
		"PkgName":      pkgName,
		"IntArithMuts": byOperator(muts, "int_arith"),
		"IntCmpMuts":   byOperator(muts, "int_cmp_boundary", "int_cmp_negate"),
		"BoolLogicMuts": byOperator(muts, "bool_logic"),
		"BoolNotMuts":  byOperator(muts, "bool_not"),
	}); err != nil {
		return "", err
	}
	return buf.String(), nil
}
