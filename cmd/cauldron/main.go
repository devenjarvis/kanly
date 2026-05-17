package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/devenjarvis/cauldron/internal/mutation"
	_ "github.com/devenjarvis/cauldron/internal/operators" // register operators via init()
	"github.com/devenjarvis/cauldron/internal/report"
	"github.com/devenjarvis/cauldron/internal/runner"
	"github.com/devenjarvis/cauldron/internal/schema"
	"github.com/devenjarvis/cauldron/internal/source"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("cauldron", flag.ContinueOnError)
	fs.SetOutput(stderr)
	formatFlag := fs.String("format", "text", "output format: text|json")
	timeoutFlag := fs.Duration("timeout", 30*time.Second, "per-mutant test timeout")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(stderr, "usage: cauldron [--format=text|json] [--timeout=30s] <package-path>\n")
		return 2
	}
	if fs.NArg() < 1 {
		fmt.Fprintf(stderr, "usage: cauldron [--format=text|json] [--timeout=30s] <package-path>\n")
		return 2
	}

	pkgPath := fs.Arg(0)

	pkg, err := source.Load(pkgPath)
	if err != nil {
		fmt.Fprintf(stderr, "error loading package: %v\n", err)
		return 1
	}

	ops := mutation.Operators()
	if len(ops) == 0 {
		fmt.Fprintf(stderr, "no operators registered\n")
		return 1
	}

	rew, err := schema.Rewrite(pkg, ops)
	if err != nil {
		fmt.Fprintf(stderr, "schema rewrite: %v\n", err)
		return 1
	}

	if len(rew.Mutations) == 0 {
		fmt.Fprintf(stdout, "no mutations found\n")
		return 0
	}

	overlayPath, cleanOverlay, err := runner.BuildOverlay(rew, pkg.Dir)
	if err != nil {
		fmt.Fprintf(stderr, "build overlay: %v\n", err)
		return 1
	}
	defer cleanOverlay()

	ctx := context.Background()
	binaryPath, cleanBin, err := runner.CompileTestBinary(ctx, pkg.ImportPath, overlayPath)
	if err != nil {
		fmt.Fprintf(stderr, "compile test binary: %v\n", err)
		return 1
	}
	defer cleanBin()

	if err := runner.RunBaseline(ctx, binaryPath, *timeoutFlag); err != nil {
		fmt.Fprintf(stderr, "baseline failed: %v\n", err)
		return 1
	}

	var results []mutation.Result
	for _, mut := range rew.Mutations {
		status, killingTests, dur, err := runner.RunMutant(ctx, binaryPath, mut.ID, *timeoutFlag)
		if err != nil {
			fmt.Fprintf(stderr, "run mutant %d: %v\n", mut.ID, err)
			return 1
		}
		results = append(results, mutation.Result{
			Mutation:     mut,
			Status:       status,
			KillingTests: killingTests,
			Duration:     dur,
		})
	}

	r := report.Build(results)

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
