package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/devenjarvis/kanly/internal/diff"
	"github.com/devenjarvis/kanly/internal/mutation"
	_ "github.com/devenjarvis/kanly/internal/operators" // register operators via init()
	"github.com/devenjarvis/kanly/internal/report"
	"github.com/devenjarvis/kanly/internal/runner"
	"github.com/devenjarvis/kanly/internal/schema"
	"github.com/devenjarvis/kanly/internal/source"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func runPackage(ctx context.Context, pkg *source.Package, ops []mutation.Operator, filter func(string, int) bool, timeout time.Duration, stderr io.Writer) ([]mutation.Result, []string, error) {
	rew, err := schema.Rewrite(pkg, ops, filter)
	if err != nil {
		return nil, nil, fmt.Errorf("schema rewrite: %w", err)
	}

	if len(rew.Mutations) == 0 {
		return nil, nil, nil
	}

	overlayPath, cleanOverlay, err := runner.BuildOverlay(rew, pkg.Dir)
	if err != nil {
		return nil, nil, fmt.Errorf("build overlay: %w", err)
	}
	defer cleanOverlay()

	binaryPath, cleanBin, err := runner.CompileTestBinary(ctx, pkg.ImportPath, overlayPath)
	if err != nil {
		return nil, nil, fmt.Errorf("compile test binary: %w", err)
	}
	defer cleanBin()

	inventory, err := runner.RunBaseline(ctx, binaryPath, pkg.Dir, timeout)
	if err != nil {
		return nil, nil, fmt.Errorf("baseline failed: %w", err)
	}

	var results []mutation.Result
	for _, mut := range rew.Mutations {
		status, killingTests, dur, err := runner.RunMutant(ctx, binaryPath, mut.ID, pkg.Dir, timeout)
		if err != nil {
			return nil, nil, fmt.Errorf("run mutant %d: %w", mut.ID, err)
		}
		results = append(results, mutation.Result{
			Mutation:     mut,
			Status:       status,
			KillingTests: killingTests,
			Duration:     dur,
		})
	}
	return results, inventory, nil
}

func run(args []string, stdout, stderr io.Writer) int {
	const usage = "usage: kanly [--format=text|json] [--timeout=30s] [--diff [--diff-base=<ref>]] <pattern>...\n"

	fs := flag.NewFlagSet("kanly", flag.ContinueOnError)
	fs.SetOutput(stderr)
	formatFlag := fs.String("format", "text", "output format: text|json")
	timeoutFlag := fs.Duration("timeout", 30*time.Second, "per-mutant test timeout")
	diffFlag := fs.Bool("diff", false, "only mutate lines changed since --diff-base")
	diffBaseFlag := fs.String("diff-base", "HEAD", "git ref to diff against when --diff is set")

	if err := fs.Parse(args); err != nil {
		fmt.Fprint(stderr, usage)
		return 2
	}

	patterns := fs.Args()
	var filter func(string, int) bool

	if *diffFlag {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(stderr, "getwd: %v\n", err)
			return 1
		}
		d, err := diff.Compute(cwd, *diffBaseFlag)
		if err != nil {
			fmt.Fprintf(stderr, "compute diff: %v\n", err)
			return 1
		}
		filter = d.Includes
		if len(d.Files()) == 0 {
			return writeReport(stdout, stderr, *formatFlag, report.Build(nil, nil))
		}
		if len(patterns) == 0 {
			patterns = d.Patterns(cwd)
			if len(patterns) == 0 {
				return writeReport(stdout, stderr, *formatFlag, report.Build(nil, nil))
			}
		}
	} else if fs.NArg() < 1 {
		fmt.Fprint(stderr, usage)
		return 2
	}

	pkgs, err := source.LoadAll("", patterns...)
	if err != nil {
		fmt.Fprintf(stderr, "error loading packages: %v\n", err)
		return 1
	}
	if len(pkgs) == 0 {
		fmt.Fprintf(stderr, "no packages matched\n")
		return 1
	}

	ops := mutation.Operators()
	if len(ops) == 0 {
		fmt.Fprintf(stderr, "no operators registered\n")
		return 1
	}

	ctx := context.Background()
	var allResults []mutation.Result
	testInventory := make(map[string][]string)

	for _, pkg := range pkgs {
		testFiles, err := filepath.Glob(filepath.Join(pkg.Dir, "*_test.go"))
		if err != nil || len(testFiles) == 0 {
			fmt.Fprintf(stderr, "skip %s: no test files\n", pkg.ImportPath)
			continue
		}

		results, inventory, err := runPackage(ctx, pkg, ops, filter, *timeoutFlag, stderr)
		if err != nil {
			fmt.Fprintf(stderr, "error in %s: %v\n", pkg.ImportPath, err)
			return 1
		}
		if len(results) == 0 {
			fmt.Fprintf(stderr, "skip %s: no mutations\n", pkg.ImportPath)
			continue
		}
		allResults = append(allResults, results...)
		testInventory[pkg.ImportPath] = inventory
	}

	return writeReport(stdout, stderr, *formatFlag, report.Build(allResults, testInventory))
}

func writeReport(stdout, stderr io.Writer, format string, r report.Report) int {
	switch format {
	case "json":
		if err := report.WriteJSON(stdout, r); err != nil {
			fmt.Fprintf(stderr, "write JSON: %v\n", err)
			return 1
		}
	default:
		if err := report.WriteText(stdout, r); err != nil {
			fmt.Fprintf(stderr, "write text: %v\n", err)
			return 1
		}
	}
	return 0
}
