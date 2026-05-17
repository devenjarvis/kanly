package errpkg

import "errors"

// ReturnErr returns its argument as-is. Expected: 1 candidate (err→nil).
func ReturnErr(err error) error {
	return err
}

// ReturnTwo returns (int, error). Expected: 1 candidate on the err expression only.
func ReturnTwo() (int, error) {
	err := errors.New("boom")
	return 0, err
}

// ReturnTwoErrors returns two error values. Expected: 2 candidates.
func ReturnTwoErrors() (error, error) {
	var a, b error = errors.New("a"), errors.New("b")
	return a, b
}

// ReturnCall returns an error-typed call expression. Expected: 1 candidate.
func ReturnCall() error {
	return errors.New("x")
}

// ReturnNil returns the nil literal. Expected: 0 candidates.
func ReturnNil() error {
	return nil
}

// ReturnInt returns a non-error. Expected: 0 candidates.
func ReturnInt() int {
	return 42
}

// ReturnIntAndNil mixes a non-error and a literal nil. Expected: 0 candidates.
func ReturnIntAndNil() (int, error) {
	return 1, nil
}

// MyErr is a concrete type implementing error. The strict-identity policy means
// returns whose static type is MyErr (not the universe error interface) are skipped.
type MyErr struct{}

func (MyErr) Error() string { return "myerr" }

// ReturnNamed returns a concrete error implementation. Expected: 0 candidates.
func ReturnNamed() MyErr {
	return MyErr{}
}

// UseOutsideReturn touches an error outside a return statement. Expected: 0 candidates.
func UseOutsideReturn(err error) bool {
	if err != nil {
		return true
	}
	return false
}
