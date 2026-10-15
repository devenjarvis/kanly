// Package corpus_test checks the mutant schema against pinned real-world
// modules. Fixture packages only contain constructs someone thought to
// write; real code finds the rest. Every regression in the P0 review
// (non-compiling rewrites, go.mod versions older than generics, baselines
// broken by instrumentation) was invisible to the fixtures and visible here.
//
// The test downloads modules, so it only runs with KANLY_CORPUS=1:
//
//	KANLY_CORPUS=1 go test -timeout 30m ./internal/corpus/
package corpus_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/devenjarvis/kanly/internal/mutation"
	_ "github.com/devenjarvis/kanly/internal/operators" // register operators via init()
	"github.com/devenjarvis/kanly/internal/runner"
	"github.com/devenjarvis/kanly/internal/schema"
	"github.com/devenjarvis/kanly/internal/source"
)

// corpus pins each module to an exact version so results are reproducible.
// knownBaselineFailures maps an import path to why its baseline is expected
// to fail under the schema; a listed package that starts passing fails the
// test so the entry gets removed.
var corpus = []struct {
	module, version       string
	knownBaselineFailures map[string]string
}{
	{module: "github.com/google/uuid", version: "v1.6.0"},
	{module: "github.com/spf13/pflag", version: "v1.0.5"},
	{module: "github.com/BurntSushi/toml", version: "v1.4.0"},
	{module: "github.com/shopspring/decimal", version: "v1.4.0"},
	{module: "github.com/go-chi/chi/v5", version: "v5.1.0"},
	{module: "github.com/google/go-cmp", version: "v0.6.0"},           // reflection-heavy
	{module: "github.com/hashicorp/golang-lru/v2", version: "v2.0.7"}, // generics
	{module: "github.com/gorilla/mux", version: "v1.8.1"},
	{module: "golang.org/x/sync", version: "v0.14.0"}, // concurrency
	{module: "github.com/dustin/go-humanize", version: "v1.0.1"},
	{
		module: "github.com/cespare/xxhash/v2", version: "v2.3.0",
		knownBaselineFailures: map[string]string{
			"github.com/cespare/xxhash/v2": "dispatcher closures allocate; TestAllocs requires zero allocations",
		},
	},
}

func TestCorpus(t *testing.T) {
	if os.Getenv("KANLY_CORPUS") == "" {
		t.Skip("set KANLY_CORPUS=1 to run against downloaded real-world modules")
	}
	for _, m := range corpus {
		t.Run(m.module+"@"+m.version, func(t *testing.T) {
			dir := fetchModule(t, m.module, m.version)
			checkModule(t, dir, m.knownBaselineFailures)
		})
	}
}

// fetchModule downloads module@version and returns a writable copy of it
// whose dependencies are also downloaded. Corpus modules must ship their
// _test.go files in the module zip (samber/lo does not) and pass their own
// suite on current Go without Kanly (pkg/errors v0.9.1 does not).
func fetchModule(t *testing.T, module, version string) string {
	t.Helper()
	out, err := exec.Command("go", "mod", "download", "-json", module+"@"+version).Output()
	if err != nil {
		t.Fatalf("go mod download %s@%s: %v", module, version, err)
	}
	var info struct{ Dir string }
	if err := json.Unmarshal(out, &info); err != nil || info.Dir == "" {
		t.Fatalf("go mod download %s@%s: no Dir in %s", module, version, out)
	}
	// The module cache is read-only; copy so go can maintain go.sum. Keep
	// the import path in the copy's location, since some suites assert on
	// file paths in stack traces.
	dir := filepath.Join(t.TempDir(), filepath.FromSlash(module))
	if err := os.CopyFS(dir, os.DirFS(info.Dir)); err != nil {
		t.Fatalf("copy module: %v", err)
	}
	// `go list` fetches what the packages and their tests import, and
	// unlike `go mod download` works for modules without requirements.
	cmd := exec.Command("go", "list", "-deps", "-test", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("download dependencies: %v\n%s", err, out)
	}
	return dir
}

// checkModule rewrites every tested package in dir with all operators and
// requires the schema to compile and its baseline to pass.
func checkModule(t *testing.T, dir string, knownBaselineFailures map[string]string) {
	t.Helper()
	// CompileTestBinary runs `go test -c <import path>` from the working
	// directory, so it must be inside the module.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)

	pkgs, err := source.LoadAll(dir, "./...")
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	checked := 0
	for _, pkg := range pkgs {
		if tests, _ := filepath.Glob(filepath.Join(pkg.Dir, "*_test.go")); len(tests) == 0 {
			continue
		}
		rew, err := schema.Rewrite(pkg, mutation.Operators(), nil)
		if err != nil {
			t.Errorf("%s: Rewrite: %v", pkg.ImportPath, err)
			continue
		}
		if len(rew.Mutations) == 0 {
			continue
		}
		checked++
		checkPackage(t, pkg, rew, knownBaselineFailures[pkg.ImportPath])
	}
	if checked == 0 {
		t.Fatal("no packages with tests and mutations")
	}
}

func checkPackage(t *testing.T, pkg *source.Package, rew *schema.Rewritten, knownFailure string) {
	t.Helper()
	overlayPath, cleanup, err := runner.BuildOverlay(rew, pkg.Dir)
	if err != nil {
		t.Errorf("%s: BuildOverlay: %v", pkg.ImportPath, err)
		return
	}
	defer cleanup()

	ctx := context.Background()
	binaryPath, cleanBin, err := runner.CompileTestBinary(ctx, pkg.ImportPath, overlayPath, pkg.GoVersion)
	if err != nil {
		t.Errorf("%s: schema does not compile (%d mutations): %v", pkg.ImportPath, len(rew.Mutations), err)
		return
	}
	defer cleanBin()

	_, err = runner.RunBaseline(ctx, binaryPath, pkg.Dir, 5*time.Minute, "")
	switch {
	case knownFailure == "" && err != nil:
		t.Errorf("%s: baseline fails under the schema: %v", pkg.ImportPath, err)
	case knownFailure != "" && err == nil:
		t.Errorf("%s: baseline now passes; remove it from knownBaselineFailures (%s)", pkg.ImportPath, knownFailure)
	case knownFailure != "":
		t.Logf("%s: known baseline failure: %s", pkg.ImportPath, knownFailure)
	default:
		t.Logf("%s: ok (%d mutations)", pkg.ImportPath, len(rew.Mutations))
	}
}
