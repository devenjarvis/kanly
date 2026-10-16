package schema

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"
)

// ignoreDirective is the comment that suppresses mutants:
//
//	x := a + b //kanly:ignore                  this line, every operator
//	//kanly:ignore int_arith -- equivalent     the next line, int_arith only
//	func f() {...}                             (in f's doc comment) all of f
//
// Operator names are comma- or space-separated; text after "--" is a free
// reason. An unknown operator name is an error so a typo cannot silently
// suppress nothing.
const ignoreDirective = "//kanly:ignore"

// ignoreSet is a set of operator names; nil means every operator.
type ignoreSet map[string]bool

func (s ignoreSet) covers(op string) bool { return s == nil || s[op] }

// ignores records a file's //kanly:ignore directives.
type ignores struct {
	lines map[int]ignoreSet // line → operators ignored on it
	funcs []ignoredFunc
}

type ignoredFunc struct {
	start, end token.Pos
	ops        ignoreSet
}

// covers reports whether a candidate of operator op at pos/line is ignored.
func (ig *ignores) covers(op string, pos token.Pos, line int) bool {
	if set, ok := ig.lines[line]; ok && set.covers(op) {
		return true
	}
	for _, f := range ig.funcs {
		if pos >= f.start && pos < f.end && f.ops.covers(op) {
			return true
		}
	}
	return false
}

// parseIgnoreDirective returns the operators a comment ignores, ok=false when
// it is not a //kanly:ignore directive.
func parseIgnoreDirective(text string, known map[string]bool) (set ignoreSet, ok bool, err error) {
	rest, found := strings.CutPrefix(text, ignoreDirective)
	if !found || (rest != "" && rest[0] != ' ' && rest[0] != '\t') {
		return nil, false, nil
	}
	rest, _, _ = strings.Cut(rest, "--")
	names := strings.FieldsFunc(rest, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' })
	if len(names) == 0 {
		return nil, true, nil
	}
	set = make(ignoreSet, len(names))
	for _, n := range names {
		if !known[n] {
			return nil, true, fmt.Errorf("unknown operator %q in %s (put free text after \"--\")", n, ignoreDirective)
		}
		set[n] = true
	}
	return set, true, nil
}

// collectIgnores reads file's //kanly:ignore directives. known is the set of
// valid operator names.
func collectIgnores(fset *token.FileSet, file *ast.File, known map[string]bool) (*ignores, error) {
	ig := &ignores{lines: make(map[int]ignoreSet)}
	add := func(line int, set ignoreSet) {
		prev, seen := ig.lines[line]
		switch {
		case !seen:
			ig.lines[line] = set
		case prev == nil || set == nil:
			ig.lines[line] = nil
		default:
			for op := range set {
				prev[op] = true
			}
		}
	}

	docOf := make(map[*ast.CommentGroup]*ast.FuncDecl)
	for _, d := range file.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Doc != nil {
			docOf[fd.Doc] = fd
		}
	}

	// Lines holding code, so a directive can tell whether it trails code
	// or stands alone above the line it applies to.
	codeLines := make(map[int]bool)
	ast.Inspect(file, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		switch n.(type) {
		case *ast.Comment, *ast.CommentGroup:
			return false
		}
		codeLines[fset.Position(n.Pos()).Line] = true
		codeLines[fset.Position(n.End()).Line] = true
		return true
	})

	for _, cg := range file.Comments {
		for _, c := range cg.List {
			set, ok, err := parseIgnoreDirective(c.Text, known)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", fset.Position(c.Pos()), err)
			}
			if !ok {
				continue
			}
			if fd, isDoc := docOf[cg]; isDoc {
				ig.funcs = append(ig.funcs, ignoredFunc{start: fd.Pos(), end: fd.End(), ops: set})
				continue
			}
			line := fset.Position(c.Pos()).Line
			if codeLines[line] {
				add(line, set)
			} else {
				add(line+1, set)
			}
		}
	}
	return ig, nil
}
