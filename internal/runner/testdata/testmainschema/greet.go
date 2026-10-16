// Package testmainschema's TestMain fails only under Kanly's schema: it
// checks record's caller, which call_delete's closure changes.
package testmainschema

import "runtime"

var LastCaller string

func record() {
	pc, _, _, _ := runtime.Caller(1)
	LastCaller = runtime.FuncForPC(pc).Name()
}

func Greet() {
	record()
}
