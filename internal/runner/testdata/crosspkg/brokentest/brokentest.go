// Package brokentest's tests do not compile.
package brokentest

import "crosspkg/lib"

// Sum adds through lib.
func Sum(a, b int) int { return lib.Add(a, b) }
