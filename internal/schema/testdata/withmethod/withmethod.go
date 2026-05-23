package withmethod

type T struct{ X int }

func Plain(a, b int) int { return a + b }
func (T) Method(a int) int { return a + 1 }
func (*T) PtrMethod(a int) int { return a - 1 }
