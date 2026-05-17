package errsample

import "testing"

// TestValidate only checks the happy path. It does not call Validate(-1), so
// it cannot detect that Validate's error-return path has been short-circuited
// to nil — the err→nil mutant on the negative branch survives.
func TestValidate(t *testing.T) {
	if err := Validate(5); err != nil {
		t.Fatalf("Validate(5) returned %v, want nil", err)
	}
}

// TestProcess asserts that Process surfaces a non-nil error. Flipping err to
// nil makes this assertion fail, so the mutation is killed.
func TestProcess(t *testing.T) {
	_, err := Process(7)
	if err == nil {
		t.Fatal("Process returned nil error, want non-nil")
	}
}
