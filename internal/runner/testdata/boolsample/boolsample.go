package boolsample

func Both(a, b bool) bool   { return a && b }
func Either(a, b bool) bool { return a || b }
func Negate(a bool) bool    { return !a }
