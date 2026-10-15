// Package initsample computes values during package initialization. The
// file is named a.go so it sorts before the generated kanly_schema.go: Go
// runs init() functions in file order, which once let these init() bodies
// run before the dispatcher had read KANLY_MUTANT.
package initsample

var base = 3

// Total is set by a package-level variable initializer.
var Total = base + 2

// Inited is set by an init() function.
var Inited int

func init() { Inited = base * 2 }
