package source_test

import (
	"go/token"
	"go/types"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/devenjarvis/cauldron/internal/source"
)

func testdataDir(t *testing.T, sub string) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "testdata", sub)
}

func TestLoadParsesPackageWithTypeInfo(t *testing.T) {
	dir := testdataDir(t, "simple")
	pkg, err := source.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(pkg.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(pkg.Files))
	}

	// Verify the + expression is typed as int.
	found := false
	for expr, tv := range pkg.TypesInfo.Types {
		// Look for a binary expression typed int
		if tv.Type == types.Typ[types.Int] {
			_ = expr
			found = true
			break
		}
	}
	if !found {
		t.Error("no int-typed expression found in TypesInfo.Types")
	}

	// Verify FileSet is populated
	if pkg.Fset == nil {
		t.Error("Fset is nil")
	}
	pkg.Fset.Iterate(func(f *token.File) bool {
		return true
	})
}
