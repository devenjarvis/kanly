// Package broken has a test that fails without any mutant, so its baseline fails.
package broken

func Sub(a, b int) int { return a - b }
