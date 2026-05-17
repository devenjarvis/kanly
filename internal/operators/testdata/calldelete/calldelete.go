package calldelete

// helper is a void-returning function used as a target for deletion in several cases below.
func helper() {}

// BareCall has a single ExprStmt-wrapped call. Expected: 1 candidate.
func BareCall() {
	helper()
}

type recv struct{}

func (recv) method() {}

// MethodCall has a method call as an ExprStmt. Expected: 1 candidate.
func MethodCall() {
	var r recv
	r.method()
}

func genericFn[T any](x T) {}

// GenericCall has a generic function instantiation as an ExprStmt. Expected: 1 candidate.
func GenericCall() {
	genericFn[int](1)
}

// IfInit has a void call in the if-statement init slot (still an ExprStmt under the hood).
// Expected: 1 candidate.
func IfInit() bool {
	if helper(); true {
		return true
	}
	return false
}

// CloseCh deletes a close() call. close is NOT excluded — deletion is observable. Expected: 1 candidate.
func CloseCh() {
	ch := make(chan int)
	close(ch)
}

// DeleteEntry deletes a delete() call. delete is NOT excluded. Expected: 1 candidate.
func DeleteEntry() {
	m := map[string]int{"a": 1}
	delete(m, "a")
}

// ClearSlice deletes a clear() call. clear is NOT excluded. Expected: 1 candidate.
func ClearSlice() {
	s := []int{1, 2, 3}
	clear(s)
}

// RecoverCall has recover() in a deferred function (1 candidate) and panic() in the body
// (0 candidates — panic excluded). Expected: 1 candidate.
func RecoverCall() {
	defer func() {
		recover()
	}()
	panic("x")
}

func helper2(x int) {}

func sideEffect() int { return 1 }

// NestedCall: only the OUTER ExprStmt-wrapped call is a candidate; the inner sideEffect()
// is not in ExprStmt position. Expected: 1 candidate.
func NestedCall() {
	helper2(sideEffect())
}

// --- Negative cases ---

// PanicCall: panic is excluded. Expected: 0 candidates.
func PanicCall() {
	panic("excluded")
}

// PrintCall: print is excluded. Expected: 0 candidates.
func PrintCall() {
	print("excluded")
}

// PrintlnCall: println is excluded. Expected: 0 candidates.
func PrintlnCall() {
	println("excluded")
}

// DeferCall: defer-wrapped calls are *ast.DeferStmt, not *ast.ExprStmt. Expected: 0 candidates.
func DeferCall() {
	defer helper()
}

// GoCall: go-wrapped calls are *ast.GoStmt, not *ast.ExprStmt. Expected: 0 candidates.
func GoCall() {
	go helper()
}

func boolFn() bool { return true }

// AssignCall: result is assigned; the surrounding statement is *ast.AssignStmt. Expected: 0 candidates.
func AssignCall() {
	_ = sideEffect()
}

// IfCondCall: call is the if condition, not an ExprStmt. Expected: 0 candidates.
func IfCondCall() {
	if boolFn() {
		return
	}
}

// MixedOps contains both a void call (call_delete target) and an arithmetic op
// (int_arith target) — used by the rewriter integration test to verify operators
// can coexist on the same file. call_delete candidates: 1 (helper()).
func MixedOps(a, b int) int {
	helper()
	return a + b
}
