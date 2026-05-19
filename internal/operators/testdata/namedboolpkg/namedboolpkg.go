package namedboolpkg

type MyBool bool

var m1, m2 MyBool

func MyAnd() MyBool { return m1 && m2 } // boolOperands: true (named type, underlying bool)
func MyNot() MyBool { return !m1 }      // boolOperand: true (named type, underlying bool)
