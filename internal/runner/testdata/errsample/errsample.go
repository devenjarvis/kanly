package errsample

import "errors"

// Validate returns an error when n is negative. The accompanying test only
// exercises the happy path, so the err→nil mutation on the negative branch
// survives — exactly the pattern this operator is designed to surface.
func Validate(n int) error {
	if n < 0 {
		return errors.New("negative")
	}
	return nil
}

// Process always surfaces a non-nil error. The accompanying test asserts
// err != nil, which kills the err→nil mutation on this return.
func Process(in int) (int, error) {
	return 0, errors.New("always")
}
