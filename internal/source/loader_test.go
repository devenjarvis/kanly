package source_test

import (
	"go/token"
	"go/types"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/devenjarvis/cauldron/internal/source"
)

// relDir returns the absolute path of sub relative to this test file's directory.
func relDir(t *testing.T, sub string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	abs, err := filepath.Abs(filepath.Join(filepath.Dir(file), sub))
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func TestLoadAllExpandsDotDotDot(t *testing.T) {
	// testdata/multipkg has foo/, bar/, and testonly/ (only _test.go files).
	// LoadAll must skip testonly/ — it has no non-test source files — and
	// return exactly 2 packages.
	pkgs, err := source.LoadAll(relDir(t, "testdata/multipkg"), "./...")
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(pkgs) != 2 {
		t.Fatalf("expected 2 packages (testonly dir must be skipped), got %d", len(pkgs))
	}
	want := map[string]bool{
		"example.com/multipkg/foo": true,
		"example.com/multipkg/bar": true,
	}
	for _, pkg := range pkgs {
		if !want[pkg.ImportPath] {
			t.Errorf("unexpected package %q", pkg.ImportPath)
		}
		delete(want, pkg.ImportPath)
	}
	for path := range want {
		t.Errorf("missing expected package %q", path)
	}
}

func TestLoadAllSingleDirPath(t *testing.T) {
	pkgs, err := source.LoadAll("", relDir(t, "testdata/simple"))
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(pkgs) != 1 {
		t.Fatalf("expected 1 package, got %d", len(pkgs))
	}
	if pkgs[0].ImportPath == "" {
		t.Error("ImportPath is empty")
	}
}

func TestLoadReturnsAbsoluteDir(t *testing.T) {
	// Relative path — Dir must still be absolute so overlay keys are valid.
	pkg, err := source.Load("testdata/simple")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !filepath.IsAbs(pkg.Dir) {
		t.Errorf("expected absolute Dir, got %q", pkg.Dir)
	}
}

func TestLoadParsesPackageWithTypeInfo(t *testing.T) {
	dir := relDir(t, "testdata/simple")
	pkg, err := source.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(pkg.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(pkg.Files))
	}

	// Verify at least one int-typed expression is in TypesInfo.
	found := false
	for _, tv := range pkg.TypesInfo.Types {
		if tv.Type == types.Typ[types.Int] {
			found = true
			break
		}
	}
	if !found {
		t.Error("no int-typed expression found in TypesInfo.Types")
	}

	if pkg.Fset == nil {
		t.Error("Fset is nil")
	}
	pkg.Fset.Iterate(func(f *token.File) bool {
		return true
	})
}
