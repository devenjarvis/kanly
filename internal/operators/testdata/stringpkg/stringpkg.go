package stringpkg

import "fmt"

// Greet returns a non-empty literal. Expected: 1 candidate.
func Greet() string { return "hello" }

// Empty returns the empty string. Expected: 0 candidates.
func Empty() string { return "" }

// Log uses a format string argument. Expected: 1 candidate on the format literal.
func Log(x int) string { return fmt.Sprintf("x=%d", x) }

// Tagged carries a struct tag. Expected: 0 candidates (struct tags are skipped).
type Tagged struct {
	F int `json:"f"`
}

// Raw returns a backtick raw string. Expected: 1 candidate.
func Raw() string { return `raw` }

// EmptyRaw returns an empty raw string. Expected: 0 candidates.
func EmptyRaw() string { return `` }

// Name is a const initializer. Expected: 0 candidates.
const Name = "alice"
