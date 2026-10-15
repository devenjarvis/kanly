// Package typeerr does not type-check, so loading it fails.
package typeerr

func Mul(a, b int) int { return a * undefinedName }
