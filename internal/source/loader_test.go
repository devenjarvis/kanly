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
