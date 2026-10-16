//go:build !unix

package runner

import "os/exec"

// setProcessGroup is a no-op where process groups are unavailable; the
// output-drain bound in testCommand still applies.
func setProcessGroup(*exec.Cmd) {}

func killProcessGroup(*exec.Cmd) {}
