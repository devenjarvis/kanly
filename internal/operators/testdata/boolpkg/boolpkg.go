package boolpkg

var b1, b2 bool

func And() bool      { return b1 && b2 }      // boolOperands: true (typed bool vars)
func Or() bool       { return b1 || b2 }       // boolOperands: true (typed bool vars)
func Not() bool      { return !b1 }            // boolOperand: true (typed bool var)
func ConstAnd() bool { return true && false }  // boolOperands: false (untyped bool constants)
func ConstNot() bool { return !true }          // boolOperand: false (untyped bool constant)
