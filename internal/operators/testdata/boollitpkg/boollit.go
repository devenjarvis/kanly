package boollitpkg

// ReturnTrue returns the literal true at a typed-bool return position.
// Expected: 1 candidate (true→false).
func ReturnTrue() bool { return true }

// ReturnFalse — typed-bool return position. Expected: 1 candidate.
func ReturnFalse() bool { return false }

// AssignTypedBool — explicit `bool` type on the var fixes the literal's
// contextual type. Expected: 1 candidate.
func AssignTypedBool() bool {
	var x bool = true
	return x
}

// ShortDeclTrue — the type-checker assigns the literal `true` the default
// typed-bool type at short-decl boundaries (the var's type is inferred from
// the constant's default type, and the literal itself gets the typed form).
// Expected: 1 candidate.
func ShortDeclTrue() bool {
	x := true
	return x
}

// CallArgTypedBool — function-arg boundary forces the literal to typed bool.
// Expected: 1 candidate.
func CallArgTypedBool() bool {
	return passBool(false)
}

func passBool(b bool) bool { return b }

// === Skipped cases ===

// ConstBool — const-decl initializers must be constant expressions.
// Expected: 0.
const ConstBool = true

// IfCondLiteral — the `if` condition position does not impose a typed-bool
// boundary on the predeclared `true` identifier; the type-checker keeps it
// untyped, and strict-identity excludes untyped-bool positions to keep the
// wrap call's return type predictable. Expected: 0.
func IfCondLiteral() int {
	if true {
		return 1
	}
	return 0
}

// MyBool — assignment to a named-bool type fixes the literal's contextual
// type to MyBool (not types.Bool); the wrap helper returns plain `bool` and
// could not satisfy a MyBool context, so this is skipped. Expected: 0.
type MyBool bool

func NamedAssignment() MyBool {
	var v MyBool = true
	return v
}
