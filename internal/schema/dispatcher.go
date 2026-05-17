package schema

import (
	"bytes"
	"text/template"

	"github.com/devenjarvis/cauldron/internal/mutation"
)

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
	}
	return "a + b"
}

// RenderDispatcher renders the schema dispatcher source for the given package and mutations.
func RenderDispatcher(pkgName string, muts []mutation.Mutation) (string, error) {
	tmpl, err := template.New("dispatcher").Funcs(template.FuncMap{
		"mutantExpr": mutantExpr,
	}).Parse(dispatcherSrc)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, map[string]interface{}{
		"PkgName": pkgName,
		"Muts":    muts,
	}); err != nil {
		return "", err
	}
	return buf.String(), nil
}
