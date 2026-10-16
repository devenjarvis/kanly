package fswrite

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPath(t *testing.T) {
	// Left behind in os.TempDir on purpose.
	if err := os.WriteFile(filepath.Join(os.TempDir(), "fswrite-leftover"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	p := Path(dir, "fswrite-result.txt")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(p, dir) {
		t.Fatalf("Path(%q, ...) = %q, outside dir", dir, p)
	}
}
