package schema

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

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

// caseSpan is the expression list of one case clause, from `case` to its
// colon, and the line of the switch or select statement it belongs to.
type caseSpan struct {
	start, end token.Pos
	stmtLine   int
}

// collectCaseSpans returns the case expression lists of every switch and
// select statement in file. Type switches are left out: their case lists hold
// only types, which nothing mutates.
func collectCaseSpans(fset *token.FileSet, file *ast.File) []caseSpan {
	var spans []caseSpan
	add := func(stmt ast.Node, body *ast.BlockStmt) {
		line := fset.Position(stmt.Pos()).Line
		for _, s := range body.List {
			switch c := s.(type) {
			case *ast.CaseClause:
				spans = append(spans, caseSpan{start: c.Case, end: c.Colon, stmtLine: line})
			case *ast.CommClause:
				spans = append(spans, caseSpan{start: c.Case, end: c.Colon, stmtLine: line})
			}
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.SwitchStmt:
			add(s, s.Body)
		case *ast.SelectStmt:
			add(s, s.Body)
		}
		return true
	})
	return spans
}

// caseStmtLine returns the statement line of the innermost case expression
// list containing pos, or 0 when pos is in none.
func caseStmtLine(spans []caseSpan, pos token.Pos) int {
	line, start := 0, token.NoPos
	for _, s := range spans {
		if pos >= s.start && pos < s.end && s.start > start {
			line, start = s.stmtLine, s.start
		}
	}
	return line
}

// collectFileScopeFuncLits returns the ranges of function literals outside
// any FuncDecl (e.g. `var h = func() {...}`). Their bodies run when called,
// not during package initialization.
func collectFileScopeFuncLits(file *ast.File) []funcSpan {
	var spans []funcSpan
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		ast.Inspect(gd, func(n ast.Node) bool {
			if fl, ok := n.(*ast.FuncLit); ok {
				spans = append(spans, funcSpan{start: fl.Pos(), end: fl.End()})
				return false
			}
			return true
		})
	}
	return spans
}

// FuncRanges returns the source range of every top-level FuncDecl in pkg,
// keyed by the canonical funcDeclName form ("Foo", "(T).Bar", "(*T).Bar").
// Used by the LLM renderer to slice the enclosing function's source out of
// disk so the artifact carries enough context for an LLM to reason about
// the surviving mutant.
func FuncRanges(pkg *source.Package) map[string]mutation.FuncRange {
	out := make(map[string]mutation.FuncRange)
	for _, file := range pkg.Files {
		for _, s := range collectFuncSpans(file) {
			start := pkg.Fset.Position(s.start)
			end := pkg.Fset.Position(s.end)
			out[s.name] = mutation.FuncRange{
				File:      start.Filename,
				StartLine: start.Line,
				EndLine:   end.Line,
			}
		}
	}
	return out
}

// FuncNames returns the canonical names (as produced by funcDeclName) of every
// top-level FuncDecl in pkg, sorted. Used by CLI callers to validate
// user-supplied function selectors and suggest near misses.
func FuncNames(pkg *source.Package) []string {
	seen := make(map[string]struct{})
	for _, file := range pkg.Files {
		for _, s := range collectFuncSpans(file) {
			seen[s.name] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func inSpans(spans []funcSpan, pos token.Pos) bool {
	for _, s := range spans {
		if pos >= s.start && pos < s.end {
			return true
		}
	}
	return false
}

func isWrapper(op mutation.Operator) bool {
	_, ok := op.(mutation.Wrapper)
	return ok
}

// Rewritten holds the results of schema rewriting: rewritten file sources and the dispatcher.
type Rewritten struct {
	Files      map[string]string // absolute file path → rewritten source
	Dispatcher string            // source of the generated kanly_schema.go file
	Mutations  []mutation.Mutation
	// Ignored counts the in-scope candidates that //kanly:ignore or an
	// Options.Accepted fingerprint suppressed.
	Ignored int
	// StaleAccepted lists, sorted, the Options.Accepted fingerprints that
	// match no candidate in the package, in scope or not: their line
	// changed or went away.
	StaleAccepted []string
}

// Rewrite transforms pkg's source files into a mutant schema using the provided operators.
// It rewrites pkg's syntax trees in place, so pkg must not be rewritten again.
//
// Mutation IDs are deterministic: files are visited in path order, and every
// candidate is numbered before the filter runs. A mutant therefore keeps its
// ID across runs and across scopes (a --diff or pkg:func run reports the same
// IDs as a full run), so `--mutant=<ids>` re-runs exactly what was reported.
// IDs are unique within the package only.
// If filter is non-nil, only candidates whose (absolute file path, 1-based line
// number, enclosing function name) satisfy filter are emitted; passing nil disables
// filtering. The function name is the canonical funcDeclName form ("Foo",
// "(T).Bar", "(*T).Bar") or "" when the candidate is outside any top-level FuncDecl.
func Rewrite(pkg *source.Package, ops []mutation.Operator, filter func(file string, line int, funcName string) bool) (*Rewritten, error) {
	return RewriteWith(pkg, ops, Options{Filter: filter})
}

// Options configures RewriteWith.
type Options struct {
	// Filter, if non-nil, keeps only candidates whose (absolute file, line,
	// enclosing function) it accepts; see Rewrite.
	Filter func(file string, line int, funcName string) bool
	// Operators, if non-nil, keeps only candidates of the named operators.
	// Every operator in ops still numbers its candidates, so IDs match a run
	// without this option.
	Operators map[string]bool
	// Accepted maps the fingerprints of mutants not to report (the
	// package's IgnoreFileName file, see ReadIgnoreFile) to their reasons.
	Accepted map[string]string
}

// RewriteWith is Rewrite with every option. Candidates suppressed by a
// //kanly:ignore directive are dropped after numbering, like filtered ones,
// and counted in Rewritten.Ignored, as are candidates whose fingerprint is
// in Options.Accepted. Generated files (see ast.IsGenerated)
// are not mutated.
func RewriteWith(pkg *source.Package, ops []mutation.Operator, opts Options) (*Rewritten, error) {
	filter := opts.Filter
	known := make(map[string]bool)
	for _, op := range mutation.Operators() {
		known[op.Name()] = true
	}
	for _, op := range ops {
		known[op.Name()] = true
	}
	ignored := 0
	matched := make(map[string]bool)
	occurrences := make(map[string]int)

	mutID := 0
	var mutations []mutation.Mutation
	rewrittenFiles := make(map[string]string)

	filePaths := make([]string, 0, len(pkg.Files))
	for p := range pkg.Files {
		filePaths = append(filePaths, p)
	}
	sort.Strings(filePaths)

	for _, filePath := range filePaths {
		file := pkg.Files[filePath]
		if ast.IsGenerated(file) {
			continue
		}
		ign, err := collectIgnores(pkg.Fset, file, known)
		if err != nil {
			return nil, err
		}
		src, err := os.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("read source for fingerprints: %w", err)
		}
		lines := strings.Split(string(src), "\n")
		type entry struct {
			node ast.Node
			cand mutation.Candidate
			id   int
			op   mutation.Operator
		}
		var entries []entry

		funcSpans := collectFuncSpans(file)
		litSpans := collectFileScopeFuncLits(file)
		caseSpans := collectCaseSpans(pkg.Fset, file)

		for _, op := range ops {
			for _, c := range op.Find(file, pkg.TypesInfo) {
				pos := pkg.Fset.Position(c.Pos)
				funcName := enclosingFunc(funcSpans, c.Pos)
				mutID++
				// Fingerprints are computed for every candidate, in scope
				// or not, so stale Accepted entries are found in scoped runs.
				scope := funcName
				if scope == "" {
					scope = "file " + filepath.Base(filePath)
				}
				// The line as written in this file, ignoring //line
				// directives, which can point anywhere.
				raw := pkg.Fset.PositionFor(c.Pos, false)
				var lineText string
				if raw.Line <= len(lines) {
					lineText = lines[raw.Line-1]
				}
				// The unnumbered fingerprint keys the count of identical
				// mutants in this scope.
				same := fingerprint(scope, op.Name(), c.Original, c.Mutant, lineText, raw.Column, 0)
				occurrences[same]++
				fp := fingerprint(scope, op.Name(), c.Original, c.Mutant, lineText, raw.Column, occurrences[same])
				_, accepted := opts.Accepted[fp]
				if accepted {
					matched[fp] = true
				}
				if filter != nil && !filter(filePath, pos.Line, funcName) {
					continue
				}
				if opts.Operators != nil && !opts.Operators[op.Name()] {
					continue
				}
				if accepted || ign.covers(op.Name(), c.Pos, pos.Line) {
					ignored++
					continue
				}
				mutations = append(mutations, mutation.Mutation{
					ID:           mutID,
					Package:      pkg.ImportPath,
					File:         filePath,
					Line:         pos.Line,
					Column:       pos.Column,
					Function:     funcName,
					OperatorName: op.Name(),
					Original:     c.Original,
					Mutant:       c.Mutant,
					Fingerprint:  fp,
					PackageInit:  funcName == "" && !inSpans(litSpans, c.Pos),
					CoverLine:    caseStmtLine(caseSpans, c.Pos),
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
		// nodeGroups lists every group on a node in first-seen order, which
		// is deterministic because entries are.
		nodeGroups := make(map[ast.Node][]*rewriteGroup)
		for _, e := range entries {
			key := groupKey{node: e.node, dispatcherKey: e.op.DispatcherKey()}
			g, ok := groups[key]
			if !ok {
				g = &rewriteGroup{op: e.op, cand: e.cand}
				groups[key] = g
				nodeGroups[e.node] = append(nodeGroups[e.node], g)
			}
			g.mutIDs = append(g.mutIDs, e.id)
		}

		// When several dispatchers target one node, the shape-specific
		// rewrite (which type-asserts the original node) must run first and
		// every mutation.Wrapper then wraps the result. Two shape-specific
		// rewrites cannot compose, so refuse rather than drop mutants.
		for node, gs := range nodeGroups {
			sort.SliceStable(gs, func(i, j int) bool {
				return !isWrapper(gs[i].op) && isWrapper(gs[j].op)
			})
			if len(gs) > 1 && !isWrapper(gs[1].op) {
				return nil, fmt.Errorf("%s: operators %s and %s both rewrite the same expression",
					pkg.Fset.Position(node.Pos()), gs[0].op.Name(), gs[1].op.Name())
			}
		}

		// Post-order rewrite: children are visited (and possibly replaced) before
		// their parent. Outer operators that capture child pointers in their
		// Rewrite — e.g. int_cmp / int_arith wrapping `expr.X, expr.Y` — therefore
		// see the already-mutated children, so nested mutations like
		// int_literal-inside-int_cmp compose correctly. Pre-order traversal
		// stranded inner replacements on the detached subtree (they never
		// reached the new outer call).
		newFile := astutil.Apply(file, nil, func(cursor *astutil.Cursor) bool {
			gs, ok := nodeGroups[cursor.Node()]
			if !ok {
				return true
			}
			cur := cursor.Node()
			for _, g := range gs {
				c := g.cand
				c.Node = cur
				cur = g.op.Rewrite(c, g.mutIDs)
			}
			cursor.Replace(cur)
			return true
		})

		var buf bytes.Buffer
		if err := format.Node(&buf, pkg.Fset, newFile); err != nil {
			return nil, fmt.Errorf("format %s: %w", filePath, err)
		}
		rewrittenFiles[filePath] = raiseFileGoVersion(buf.String())
	}

	dispatcher, err := RenderDispatcher(pkg.Pkg.Name(), mutations)
	if err != nil {
		return nil, fmt.Errorf("render dispatcher: %w", err)
	}

	var stale []string
	for fp := range opts.Accepted {
		if !matched[fp] {
			stale = append(stale, fp)
		}
	}
	sort.Strings(stale)

	return &Rewritten{
		Files:         rewrittenFiles,
		Dispatcher:    dispatcher,
		Mutations:     mutations,
		Ignored:       ignored,
		StaleAccepted: stale,
	}, nil
}
