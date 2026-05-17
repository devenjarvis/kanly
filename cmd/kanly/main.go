package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

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

func runPackage(ctx context.Context, pkg *source.Package, ops []mutation.Operator, timeout time.Duration, stderr io.Writer) ([]mutation.Result, error) {
	rew, err := schema.Rewrite(pkg, ops)
	if err != nil {
		return nil, fmt.Errorf("schema rewrite: %w", err)
	}

	if len(rew.Mutations) == 0 {
		return nil, nil
	}

	overlayPath, cleanOverlay, err := runner.BuildOverlay(rew, pkg.Dir)
	if err != nil {
		return nil, fmt.Errorf("build overlay: %w", err)
	}
	defer cleanOverlay()

	binaryPath, cleanBin, err := runner.CompileTestBinary(ctx, pkg.ImportPath, overlayPath)
	if err != nil {
		return nil, fmt.Errorf("compile test binary: %w", err)
	}
	defer cleanBin()

	if err := runner.RunBaseline(ctx, binaryPath, pkg.Dir, timeout); err != nil {
		return nil, fmt.Errorf("baseline failed: %w", err)
	}

	var results []mutation.Result
	for _, mut := range rew.Mutations {
		status, killingTests, dur, err := runner.RunMutant(ctx, binaryPath, mut.ID, pkg.Dir, timeout)
		if err != nil {
			return nil, fmt.Errorf("run mutant %d: %w", mut.ID, err)
		}
		results = append(results, mutation.Result{
			Mutation:     mut,
			Status:       status,
			KillingTests: killingTests,
			Duration:     dur,
		})
	}
	return results, nil
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("kanly", flag.ContinueOnError)
	fs.SetOutput(stderr)
	formatFlag := fs.String("format", "text", "output format: text|json")
	timeoutFlag := fs.Duration("timeout", 30*time.Second, "per-mutant test timeout")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(stderr, "usage: kanly [--format=text|json] [--timeout=30s] <pattern>...\n")
		return 2
	}
	if fs.NArg() < 1 {
		fmt.Fprintf(stderr, "usage: kanly [--format=text|json] [--timeout=30s] <pattern>...\n")
		return 2
	}

	patterns := fs.Args()

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

	for _, pkg := range pkgs {
		testFiles, err := filepath.Glob(filepath.Join(pkg.Dir, "*_test.go"))
		if err != nil || len(testFiles) == 0 {
			fmt.Fprintf(stderr, "skip %s: no test files\n", pkg.ImportPath)
			continue
		}

		results, err := runPackage(ctx, pkg, ops, *timeoutFlag, stderr)
		if err != nil {
			fmt.Fprintf(stderr, "error in %s: %v\n", pkg.ImportPath, err)
			return 1
		}
		if len(results) == 0 {
			fmt.Fprintf(stderr, "skip %s: no mutations\n", pkg.ImportPath)
			continue
		}
		allResults = append(allResults, results...)
	}

	r := report.Build(allResults)

	switch *formatFlag {
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
