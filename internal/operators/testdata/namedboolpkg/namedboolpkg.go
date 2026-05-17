package namedboolpkg

type MyBool bool

var m1, m2 MyBool

func MyAnd() MyBool { return m1 && m2 } // boolOperands: false (named type, not exactly bool)
func MyNot() MyBool { return !m1 }      // boolOperand: false (named type, not exactly bool)
