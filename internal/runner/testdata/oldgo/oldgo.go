// Package oldgo is a fixture module whose go.mod predates generics (go 1.16).
// The mutant schema's generic dispatchers only compile once the runner raises
// the package's language version.
package oldgo

func Add(a, b int) int { return a + b }
