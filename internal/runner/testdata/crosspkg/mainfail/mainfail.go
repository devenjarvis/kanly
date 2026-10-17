package mainfail

import "crosspkg/lib"

// Sum adds through lib.
func Sum(a, b int) int { return lib.Add(a, b) }
