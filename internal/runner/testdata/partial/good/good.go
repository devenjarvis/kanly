// Package good is the healthy sibling in the partial fixture: its result must
// survive the failures of the packages next to it.
package good

func Add(a, b int) int { return a + b }
