package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"

	"github.com/devenjarvis/kanly/internal/mutation"
)

// concurrentTests returns the top-level functions in dir's _test.go files
// whose bodies start goroutines: a go statement, a .Go(...) call
// (sync.WaitGroup.Go, errgroup) or a .Parallel() call inside a function
// literal passed to .Run(...). A top-level t.Parallel() alone does not
// count: it is common, and the test's own steps still run in order. Only
// the test's own body is read, not the helpers it calls.
func concurrentTests(dir string) map[string]bool {
	files, _ := filepath.Glob(filepath.Join(dir, "*_test.go"))
	out := make(map[string]bool)
	for _, file := range files {
		f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if ok && fd.Recv == nil && fd.Body != nil && startsGoroutines(fd.Body) {
				out[fd.Name.Name] = true
			}
		}
	}
	return out
}

func startsGoroutines(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.GoStmt:
			found = true
		case *ast.CallExpr:
			switch methodName(x) {
			case "Go":
				found = true
			case "Run":
				for _, arg := range x.Args {
					if lit, ok := arg.(*ast.FuncLit); ok && callsParallel(lit.Body) {
						found = true
					}
				}
			}
		}
		return !found
	})
	return found
}

func callsParallel(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && methodName(call) == "Parallel" {
			found = true
		}
		return !found
	})
	return found
}

func methodName(call *ast.CallExpr) string {
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		return sel.Sel.Name
	}
	return ""
}

// markConcurrentKills sets ConcurrentKill on each kill whose every killing
// test is in concurrent, keyed by result name.
func markConcurrentKills(results []mutation.Result, concurrent map[string]bool) {
	for i := range results {
		r := &results[i]
		if r.Status != mutation.StatusKilled || len(r.KillingTests) == 0 {
			continue
		}
		all := true
		for _, name := range r.KillingTests {
			if !concurrent[name] {
				all = false
			}
		}
		r.ConcurrentKill = all
	}
}
