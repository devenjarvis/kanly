// Package selector parses positional CLI arguments of the form
// `<package-pattern>[:<func-list>]` into a Spec, and matches user-supplied
// function names against the canonical `funcDeclName` form produced by the
// schema rewriter ("Foo", "(T).Bar", "(*T).Bar").
//
// Matching is lenient so an LLM can write the most natural form without
// remembering the exact receiver syntax:
//
//	user form        matches canonical
//	---------        -----------------
//	Foo              Foo
//	T.Bar            (T).Bar and (*T).Bar
//	*T.Bar           (*T).Bar
//	(T).Bar          (T).Bar
//	(*T).Bar         (*T).Bar
package selector

import (
	"fmt"
	"strings"
)

// Spec is one parsed positional argument: a package pattern (the same string
// you'd pass to `go test` / `packages.Load`) plus an optional list of function
// names to filter mutations to.
type Spec struct {
	Pattern string
	Funcs   []string // user-supplied forms; empty if no ':' was present
}

// Parse turns positional args like ["./internal/foo:Compute,Render", "./..."]
// into Specs. A pattern that contains "..." cannot also carry a func filter;
// the rewriter only knows how to scope mutants to a single package's funcs.
func Parse(args []string) ([]Spec, error) {
	out := make([]Spec, 0, len(args))
	for _, a := range args {
		s, err := parseOne(a)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func parseOne(arg string) (Spec, error) {
	idx := splitOutsideParens(arg, ':')
	if idx < 0 {
		return Spec{Pattern: arg}, nil
	}
	pattern := strings.TrimSpace(arg[:idx])
	rest := strings.TrimSpace(arg[idx+1:])
	if pattern == "" {
		return Spec{}, fmt.Errorf("selector %q: empty package pattern before ':'", arg)
	}
	if rest == "" {
		return Spec{}, fmt.Errorf("selector %q: empty function list after ':'", arg)
	}
	if strings.Contains(pattern, "...") {
		return Spec{}, fmt.Errorf("selector %q: function filter requires a single package (got glob %q)", arg, pattern)
	}
	funcs, err := splitFuncList(rest)
	if err != nil {
		return Spec{}, fmt.Errorf("selector %q: %w", arg, err)
	}
	return Spec{Pattern: pattern, Funcs: funcs}, nil
}

// splitOutsideParens returns the index of the first occurrence of sep that is
// not nested inside `(...)`. -1 if none.
func splitOutsideParens(s string, sep byte) int {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case sep:
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// splitFuncList splits a comma-separated list of function names, respecting
// parens so `(Foo).Bar,(*Baz).Qux` parses as two entries, not four.
func splitFuncList(s string) ([]string, error) {
	var parts []string
	start := 0
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			if depth == 0 {
				return nil, fmt.Errorf("unbalanced ')' in function list %q", s)
			}
			depth--
		case ',':
			if depth == 0 {
				p := strings.TrimSpace(s[start:i])
				if p == "" {
					return nil, fmt.Errorf("empty function name in list %q", s)
				}
				parts = append(parts, p)
				start = i + 1
			}
		}
	}
	if depth != 0 {
		return nil, fmt.Errorf("unbalanced '(' in function list %q", s)
	}
	p := strings.TrimSpace(s[start:])
	if p == "" {
		return nil, fmt.Errorf("empty function name in list %q", s)
	}
	parts = append(parts, p)
	return parts, nil
}

// MatchFunc reports whether the user-supplied func entry matches the canonical
// name produced by schema.funcDeclName. See package doc for the rules.
func MatchFunc(userEntry, canonical string) bool {
	if userEntry == canonical {
		return true
	}
	// Already-qualified form with parens — must be exact.
	if strings.HasPrefix(userEntry, "(") {
		return false
	}
	// *T.Method form → (*T).Method.
	if strings.HasPrefix(userEntry, "*") {
		dot := strings.Index(userEntry, ".")
		if dot < 0 {
			return false
		}
		recv := userEntry[1:dot]
		method := userEntry[dot+1:]
		return canonical == "(*"+recv+")."+method
	}
	// T.Method → either (T).Method or (*T).Method.
	if dot := strings.Index(userEntry, "."); dot > 0 {
		recv := userEntry[:dot]
		method := userEntry[dot+1:]
		return canonical == "("+recv+")."+method ||
			canonical == "(*"+recv+")."+method
	}
	// Plain identifier — only matches a top-level func with the same name.
	return false
}

// AnyMatch returns true if canonical matches any of the supplied user entries.
func AnyMatch(entries []string, canonical string) bool {
	for _, e := range entries {
		if MatchFunc(e, canonical) {
			return true
		}
	}
	return false
}
