// Package reader's tests run lib.Half from their own directory.
package reader

import "crosspkg/lib"

// Halve halves n through lib.
func Halve(n int) int { return lib.Half(n) }
