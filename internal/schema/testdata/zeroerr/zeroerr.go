package zeroerr

import (
	"errors"
	"fmt"
)

var errBad = errors.New("bad")

func Parse(s string) (int, error) {
	if s == "" {
		return 0, fmt.Errorf("empty input")
	}
	if s == "x" {
		return -1, errBad
	}
	if s == "y" {
		return 7, errBad
	}
	return 0, nil
}

func Check(err error) (bool, int, error) {
	if err != nil {
		return false, 0, err
	}
	return true, 1, nil
}

func Named() (n int, err error) {
	f := func() (int, error) {
		return 0, errBad
	}
	n, err = f()
	return 0, err
}

func NoError() (int, bool) {
	return 0, false
}

func AfterLiteral() (int, bool) {
	check := func() (int, error) { return 1, nil }
	_, _ = check()
	return 0, true
}

var Lit = func() (int, error) {
	return 0, errBad
}

type UserID int64

func Sized() (int32, UserID, error) {
	return 0, 0, errBad
}

func Generic[T any](v T) (int, T, error) {
	return 0, v, errBad
}
