package runner

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// Dependent is a package whose test binary links another package of the
// same module, so its tests can kill that package's mutants.
type Dependent struct {
	ImportPath string
	Dir        string
}

// TestDependents maps each package of the module rooted at moduleDir to the
// module's other packages whose test binaries link it, directly or
// transitively, sorted by import path. Packages without test files have no
// test binary and never appear as dependents. buildFlags are `go list`
// flags such as -tags.
func TestDependents(ctx context.Context, moduleDir string, buildFlags ...string) (map[string][]Dependent, error) {
	args := append([]string{"list", "-e", "-test", "-f", "{{.ImportPath}}\t{{.Dir}}\t{{join .Deps \" \"}}"}, buildFlags...)
	args = append(args, "./...")
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = moduleDir
	cmd.Env = os.Environ()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list -test: %w\n%s", err, stderr.String())
	}
	return parseTestDependents(out), nil
}

// parseTestDependents reads the `go list -test` lines written by
// TestDependents. A package's test binary is listed as "<pkg>.test", and
// its Deps name packages recompiled for the test as "<pkg> [<x>.test]".
func parseTestDependents(out []byte) map[string][]Dependent {
	type entry struct{ path, dir, deps string }
	var entries []entry
	module := make(map[string]bool)
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) != 3 {
			continue
		}
		e := entry{fields[0], fields[1], fields[2]}
		entries = append(entries, e)
		if !strings.Contains(e.path, " ") && !strings.HasSuffix(e.path, ".test") {
			module[e.path] = true
		}
	}
	result := make(map[string][]Dependent)
	for _, e := range entries {
		testPkg, ok := strings.CutSuffix(e.path, ".test")
		if !ok || strings.Contains(testPkg, " ") || !module[testPkg] {
			continue
		}
		seen := make(map[string]bool)
		for _, dep := range strings.Fields(e.deps) {
			if strings.HasPrefix(dep, "[") {
				continue // the "[x.test]" half of a "pkg [x.test]" pair
			}
			if dep == testPkg || !module[dep] || seen[dep] {
				continue
			}
			seen[dep] = true
			result[dep] = append(result[dep], Dependent{ImportPath: testPkg, Dir: e.dir})
		}
	}
	for _, deps := range result {
		sort.Slice(deps, func(i, j int) bool { return deps[i].ImportPath < deps[j].ImportPath })
	}
	return result
}
