package schema

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"

	"golang.org/x/tools/go/ast/astutil"

	"github.com/devenjarvis/kanly/internal/mutation"
	"github.com/devenjarvis/kanly/internal/source"
)

// funcSpan records a top-level FuncDecl's source range and qualified name.
type funcSpan struct {
	start token.Pos
	end   token.Pos
	name  string
}

// collectFuncSpans returns the source ranges of every top-level FuncDecl in file,
// each tagged with a display name that disambiguates methods by receiver type.
func collectFuncSpans(file *ast.File) []funcSpan {
	var spans []funcSpan
	for _, decl := range file.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Name == nil {
			continue
		}
		spans = append(spans, funcSpan{
			start: fd.Pos(),
			end:   fd.End(),
			name:  funcDeclName(fd),
		})
	}
	return spans
}

// funcDeclName renders a FuncDecl as either "Func" or "(Recv).Method" / "(*Recv).Method".
func funcDeclName(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return fd.Name.Name
	}
	recv := fd.Recv.List[0].Type
	switch t := recv.(type) {
	case *ast.Ident:
		return "(" + t.Name + ")." + fd.Name.Name
	case *ast.StarExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return "(*" + id.Name + ")." + fd.Name.Name
		}
	case *ast.IndexExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return "(" + id.Name + ")." + fd.Name.Name
		}
	case *ast.IndexListExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return "(" + id.Name + ")." + fd.Name.Name
		}
	}
	return fd.Name.Name
}

// enclosingFunc returns the display name of the FuncDecl whose range contains pos,
// or "" if pos lies outside every top-level function (file-scope).
func enclosingFunc(spans []funcSpan, pos token.Pos) string {
	for _, s := range spans {
		if pos >= s.start && pos < s.end {
			return s.name
		}
	}
	return ""
}

// Rewritten holds the results of schema rewriting: rewritten file sources and the dispatcher.
type Rewritten struct {
	Files      map[string]string // absolute file path → rewritten source
	Dispatcher string            // source of the generated kanly_schema.go file
	Mutations  []mutation.Mutation
}

// Rewrite transforms pkg's source files into a mutant schema using the provided operators.
// If filter is non-nil, only candidates whose (absolute file path, line number)
// satisfy filter are emitted; passing nil disables filtering.
func Rewrite(pkg *source.Package, ops []mutation.Operator, filter func(file string, line int) bool) (*Rewritten, error) {
	mutID := 0
	var mutations []mutation.Mutation
	rewrittenFiles := make(map[string]string)

	for filePath, file := range pkg.Files {
		type entry struct {
			node ast.Node
			cand mutation.Candidate
			id   int
			op   mutation.Operator
		}
		var entries []entry

		funcSpans := collectFuncSpans(file)

		for _, op := range ops {
			for _, c := range op.Find(file, pkg.TypesInfo) {
				pos := pkg.Fset.Position(c.Pos)
				if filter != nil && !filter(filePath, pos.Line) {
					continue
				}
				mutID++
				mutations = append(mutations, mutation.Mutation{
					ID:           mutID,
					Package:      pkg.ImportPath,
					File:         filePath,
					Line:         pos.Line,
					Column:       pos.Column,
					Function:     enclosingFunc(funcSpans, c.Pos),
					OperatorName: op.Name(),
					Original:     c.Original,
					Mutant:       c.Mutant,
				})
				entries = append(entries, entry{node: c.Node, cand: c, id: mutID, op: op})
			}
		}

		if len(entries) == 0 {
			continue
		}

		// Group candidates by (node, dispatcherKey) so co-located mutations share one call site.
		type groupKey struct {
			node          ast.Node
			dispatcherKey string
		}
		type rewriteGroup struct {
			mutIDs []int
			op     mutation.Operator
			cand   mutation.Candidate
		}
		groups := make(map[groupKey]*rewriteGroup)
		for _, e := range entries {
			key := groupKey{node: e.node, dispatcherKey: e.op.DispatcherKey()}
			g, ok := groups[key]
			if !ok {
				g = &rewriteGroup{op: e.op, cand: e.cand}
				groups[key] = g
			}
			g.mutIDs = append(g.mutIDs, e.id)
		}

		// Build a flat node→group lookup for the Apply callback.
		nodeMap := make(map[ast.Node]*rewriteGroup, len(groups))
		for key, g := range groups {
			nodeMap[key.node] = g
		}

		newFile := astutil.Apply(file, func(cursor *astutil.Cursor) bool {
			g, ok := nodeMap[cursor.Node()]
			if !ok {
				return true
			}
			cursor.Replace(g.op.Rewrite(g.cand, g.mutIDs))
			return true
		}, nil)

		var buf bytes.Buffer
		if err := format.Node(&buf, pkg.Fset, newFile); err != nil {
			return nil, fmt.Errorf("format %s: %w", filePath, err)
		}
		rewrittenFiles[filePath] = buf.String()
	}

	dispatcher, err := RenderDispatcher(pkg.Pkg.Name(), mutations)
	if err != nil {
		return nil, fmt.Errorf("render dispatcher: %w", err)
	}

	return &Rewritten{
		Files:      rewrittenFiles,
		Dispatcher: dispatcher,
		Mutations:  mutations,
	}, nil
}
