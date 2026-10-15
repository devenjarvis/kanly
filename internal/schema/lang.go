package schema

import (
	"go/build/constraint"
	"go/version"
	"strings"
)

// MinLang is the lowest Go language version the mutant schema compiles
// under: the dispatcher uses generics, and its call sites rely on the
// untyped-constant inference rules added in Go 1.21.
const MinLang = "go1.21"

// raiseFileGoVersion rewrites go1.N terms older than MinLang in src's
// //go:build line to MinLang. Since Go 1.21 such a term also sets the file's
// language version, and some toolchains (go1.23.0, for one) honour versions
// before go1.18 there, rejecting the generic dispatcher calls even when the
// package's -lang is raised. Any toolchain that can build the schema
// satisfies both go1.N and MinLang, so the expression's value, and hence
// which files are built, is unchanged.
func raiseFileGoVersion(src string) string {
	lines := strings.SplitAfter(src, "\n")
	for i, line := range lines {
		trimmed := strings.TrimRight(line, "\r\n")
		if strings.HasPrefix(trimmed, "package ") {
			break
		}
		if !constraint.IsGoBuild(trimmed) {
			continue
		}
		expr, err := constraint.Parse(trimmed)
		if err != nil {
			return src
		}
		if !raiseTags(expr) {
			return src
		}
		lines[i] = "//go:build " + expr.String() + line[len(trimmed):]
		return strings.Join(lines, "")
	}
	return src
}

// raiseTags replaces go-version tags older than MinLang in place and
// reports whether it changed any.
func raiseTags(e constraint.Expr) bool {
	switch x := e.(type) {
	case *constraint.TagExpr:
		if version.IsValid(x.Tag) && version.Compare(x.Tag, MinLang) < 0 {
			x.Tag = MinLang
			return true
		}
	case *constraint.NotExpr:
		return raiseTags(x.X)
	case *constraint.AndExpr:
		l, r := raiseTags(x.X), raiseTags(x.Y)
		return l || r
	case *constraint.OrExpr:
		l, r := raiseTags(x.X), raiseTags(x.Y)
		return l || r
	}
	return false
}
