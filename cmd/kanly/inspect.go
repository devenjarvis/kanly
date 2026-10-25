package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/devenjarvis/kanly/internal/mutation"
	"github.com/devenjarvis/kanly/internal/runner"
	"github.com/devenjarvis/kanly/internal/schema"
	"github.com/devenjarvis/kanly/internal/source"
)

// `kanly show` and `kanly test` let an agent look at one mutant and try a
// test against it with plain `go test`, without a full mutation run. Both
// rewrite the package afresh, so a mutant is found by fingerprint after
// edits that renumber it.

// loadMutants loads the single package matching pattern and rewrites it
// with every operator. Mutants accepted in .kanlyignore are kept: asking
// for one by name is reason enough to show it.
func loadMutants(pattern, tags string) (*source.Package, *schema.Rewritten, error) {
	pkgs, loadErrs, err := source.LoadAllTags("", tags, pattern)
	if err != nil {
		return nil, nil, err
	}
	if len(loadErrs) > 0 {
		return nil, nil, fmt.Errorf("%s: %v", loadErrs[0].ImportPath, loadErrs[0].Err)
	}
	if len(pkgs) != 1 {
		return nil, nil, fmt.Errorf("%q must match exactly one package with non-test files; got %d", pattern, len(pkgs))
	}
	rew, err := schema.RewriteWith(pkgs[0], mutation.Operators(), schema.Options{})
	if err != nil {
		return nil, nil, err
	}
	return pkgs[0], rew, nil
}

// findMutants returns the mutants named by refs (IDs or fingerprints), in
// the order given.
func findMutants(rew *schema.Rewritten, pkg string, refs []string) ([]mutation.Mutation, error) {
	var out []mutation.Mutation
	for _, ref := range refs {
		set, err := parseMutants(ref)
		if err != nil || set == nil {
			return nil, fmt.Errorf("%q is not a mutant ID or fingerprint", ref)
		}
		found := false
		for _, m := range rew.Mutations {
			if set.has(m) {
				out = append(out, m)
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("%s has no mutant %s; a fingerprint changes when its line does, so re-run kanly on the package to see its current mutants", pkg, ref)
		}
	}
	return out, nil
}

// describeMutant writes a mutant's location, change and source line, with a
// caret under the mutated column and, when the change can be spelled out
// exactly, the line as the mutant has it.
func describeMutant(w io.Writer, m mutation.Mutation) {
	fmt.Fprintf(w, "%s:%d:%d", displayPath(m.File), m.Line, m.Column)
	if m.Function != "" {
		fmt.Fprintf(w, " in %s", m.Function)
	}
	fmt.Fprintf(w, "\n  #%d fp %s · %s `%s` → `%s`\n", m.ID, m.Fingerprint, m.OperatorName, m.Original, m.Mutant)
	data, err := os.ReadFile(m.File)
	if err != nil {
		return
	}
	lines := strings.Split(string(data), "\n")
	if m.Line < 1 || m.Line > len(lines) {
		return
	}
	line := lines[m.Line-1]
	col := m.Column - 1
	if col < 0 || col > len(line) {
		return
	}
	// Tabs stay tabs so the caret lines up however they are displayed.
	before := line[:col]
	pad := strings.Map(func(r rune) rune {
		if r == '\t' {
			return r
		}
		return ' '
	}, before)
	fmt.Fprintf(w, "  original: %s\n            %s^\n", line, pad)
	if mutated, ok := mutatedLine(line, col, m); ok {
		fmt.Fprintf(w, "  mutant:   %s\n", mutated)
	}
}

// mutatedLine returns line with the mutant applied when the mutation's
// original text is the source text at col. That holds for operator and
// literal mutations; others (a deleted call, a zeroed return) describe the
// change symbolically instead. Some symbolic originals are source text too:
// a dropped or swapped switch clause and a removed receive.
func mutatedLine(line string, col int, m mutation.Mutation) (string, bool) {
	if m.OperatorName == "switch_case" || m.OperatorName == "chan_op" {
		return "", false
	}
	if m.Original == "" || !strings.HasPrefix(line[col:], m.Original) {
		return "", false
	}
	return line[:col] + m.Mutant + line[col+len(m.Original):], true
}

// runShow implements `kanly show [--tags=...] <pkg> <mutant>...`.
func runShow(args []string, stdout, stderr io.Writer) int {
	const usage = "usage: kanly show [--tags=a,b] <pkg> <mutant-id-or-fp>...\n"
	fs := flag.NewFlagSet("kanly show", flag.ContinueOnError)
	fs.SetOutput(stderr)
	tags := fs.String("tags", "", "comma-separated build tags, as for go build -tags")
	if err := fs.Parse(args); err != nil || fs.NArg() < 2 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	if err := show(stdout, fs.Arg(0), fs.Args()[1:], *tags); err != nil {
		fmt.Fprintf(stderr, "kanly show: %v\n", err)
		return 1
	}
	return 0
}

func show(w io.Writer, pattern string, refs []string, tags string) error {
	pkg, rew, err := loadMutants(pattern, tags)
	if err != nil {
		return err
	}
	muts, err := findMutants(rew, pkg.ImportPath, refs)
	if err != nil {
		return err
	}
	for i, m := range muts {
		if i > 0 {
			fmt.Fprintln(w)
		}
		describeMutant(w, m)
	}
	fmt.Fprintf(w, "\nTry a test against it: kanly test %s %s -run <TestName> (add --in=<pkg> for a test in another package)\n", pattern, muts[0].Fingerprint)
	return nil
}

// runTest implements `kanly test [--tags=...] [--in=<pkg>] <pkg> <mutant>
// [go test flags]`: `go test` on the package, or on the package --in names,
// with the mutant applied. The exit status is go test's, so a test that
// kills the mutant makes it non-zero.
func runTest(args []string, stdout, stderr io.Writer) int {
	const usage = "usage: kanly test [--tags=a,b] [--in=<pkg>] <pkg> <mutant-id-or-fp> [go test flags...]\n"
	fs := flag.NewFlagSet("kanly test", flag.ContinueOnError)
	fs.SetOutput(stderr)
	tags := fs.String("tags", "", "comma-separated build tags, as for go build -tags")
	in := fs.String("in", "", "run this package's tests instead, such as one that imports <pkg>")
	if err := fs.Parse(args); err != nil || fs.NArg() < 2 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	cmd, cleanup, err := mutantTestCommand(fs.Arg(0), fs.Arg(1), *tags, *in, fs.Args()[2:], stderr)
	if err == nil {
		defer cleanup()
		cmd.Stdout, cmd.Stderr = stdout, stderr
		err = cmd.Run()
	}
	var exit *exec.ExitError
	switch {
	case err == nil:
		fmt.Fprintf(stderr, "kanly: the tests passed, so the mutant survives them\n")
		return 0
	case errors.As(err, &exit):
		fmt.Fprintf(stderr, "kanly: go test failed (exit %d): if a test failed, rather than the build, it kills the mutant\n", exit.ExitCode())
		return exit.ExitCode()
	default:
		fmt.Fprintf(stderr, "kanly test: %v\n", err)
		return 1
	}
}

// mutantTestCommand prepares the `go test` run of runTest and announces it
// on stderr. cleanup removes the overlay.
func mutantTestCommand(pattern, ref, tags, in string, goTestArgs []string, stderr io.Writer) (*exec.Cmd, func(), error) {
	pkg, rew, err := loadMutants(pattern, tags)
	if err != nil {
		return nil, nil, err
	}
	muts, err := findMutants(rew, pkg.ImportPath, []string{ref})
	if err != nil {
		return nil, nil, err
	}
	m := muts[0]
	overlay, cleanup, err := runner.BuildOverlay(rew, pkg.Dir)
	if err != nil {
		return nil, nil, err
	}

	// -count=1: a cached pass from before the test was edited says nothing.
	args := []string{"test", "-count=1", "-vet=off", "-overlay=" + overlay}
	args = append(args, runner.SchemaBuildFlags(pkg.ImportPath, pkg.GoVersion)...)
	if tags != "" {
		args = append(args, "-tags="+tags)
	}
	args = append(args, goTestArgs...)
	target := pkg.ImportPath
	if in != "" {
		target = in
	}
	args = append(args, target)

	fmt.Fprintf(stderr, "kanly: go test %s with mutant #%d (fp %s) applied: %s:%d:%d %s `%s` → `%s`\n",
		target, m.ID, m.Fingerprint, displayPath(m.File), m.Line, m.Column, m.OperatorName, m.Original, m.Mutant)
	cmd := exec.Command("go", args...)
	// exec uses the last value of a repeated variable, so this overrides
	// any KANLY_MUTANT already set.
	cmd.Env = append(os.Environ(), "KANLY_MUTANT="+strconv.Itoa(m.ID))
	return cmd, cleanup, nil
}
