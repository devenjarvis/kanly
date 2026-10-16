// Package failingsample's tests include ones that fail without any mutant:
// always, only under the schema, and only when run on their own.
package failingsample

import "runtime"

func Add(a, b int) int {
	return a + b
}

var lastCaller string

func record() {
	pc, _, _, _ := runtime.Caller(1)
	lastCaller = runtime.FuncForPC(pc).Name()
}

// Greet's call statement is wrapped in a closure by call_delete, which
// changes record's caller.
func Greet() {
	record()
}
