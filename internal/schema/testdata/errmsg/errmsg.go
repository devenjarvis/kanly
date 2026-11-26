package errmsg

import (
	"errors"
	. "errors"
	"fmt"
	f "fmt"
)

type logger struct{}

type hook struct{ fn func(string) error }

func newErr(s string) error { return errors.New(s) }

func (logger) Errorf(format string, args ...any) {}

func F(n int, err error) []error {
	var l logger
	l.Errorf("not an error constructor")
	return []error{
		errors.New("plain message"),
		fmt.Errorf("bad count %d", n),
		fmt.Errorf("wrap: %w", err),
		fmt.Errorf("%s: %w", "op name", err),
		errors.New(fmt.Sprintf("nested %d", n)),
		fmt.Errorf(`raw %d`, n),
		newErr("local helper"),
		hook{}.fn("field func"),
		fmt.Errorf("outer %v", errors.New("inner")),
		New("dot import"),
		f.Errorf("aliased %d", n),
		fmt.Errorf("indexed %[1]w", err),
		fmt.Errorf("flagged %+w", err),
		fmt.Errorf("100%%w done"),
		errors.New("con" + "cat"),
		func(s string) error { return nil }("func literal"),
	}
}
