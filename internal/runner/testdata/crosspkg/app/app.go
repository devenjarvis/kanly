package app

import "crosspkg/lib"

// Area scales a side by itself.
func Area(side int) int { return lib.Scale(side, side) }

// Report computes a value nobody checks.
func Report(n int) { _ = lib.Half(n) }
