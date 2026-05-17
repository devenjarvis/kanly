package schema

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"

	"golang.org/x/tools/go/ast/astutil"

	"github.com/devenjarvis/kanly/internal/mutation"
	"github.com/devenjarvis/kanly/internal/source"
)

// Rewritten holds the results of schema rewriting: rewritten file sources and the dispatcher.
type Rewritten struct {
	Files      map[string]string // absolute file path → rewritten source
	Dispatcher string            // source of the generated kanly_schema.go file
	Mutations  []mutation.Mutation
}

// Rewrite transforms pkg's source files into a mutant schema using the provided operators.
func Rewrite(pkg *source.Package, ops []mutation.Operator) (*Rewritten, error) {
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

		for _, op := range ops {
			for _, c := range op.Find(file, pkg.TypesInfo) {
				mutID++
				pos := pkg.Fset.Position(c.Pos)
				mutations = append(mutations, mutation.Mutation{
					ID:           mutID,
					Package:      pkg.ImportPath,
					File:         filePath,
					Line:         pos.Line,
					Column:       pos.Column,
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
