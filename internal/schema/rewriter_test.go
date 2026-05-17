package schema_test

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/devenjarvis/cauldron/internal/mutation"
	"github.com/devenjarvis/cauldron/internal/operators"
	"github.com/devenjarvis/cauldron/internal/schema"
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

func TestRewriteReplacesPlusWithDispatcherCall(t *testing.T) {
	pkg, err := source.Load(relDir(t, "../source/testdata/simple"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	rew, err := schema.Rewrite(pkg, []mutation.Operator{operators.IntArith{}})
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}

	if len(rew.Mutations) != 1 {
		t.Fatalf("expected 1 mutation, got %d", len(rew.Mutations))
	}
	if rew.Mutations[0].ID != 1 {
		t.Errorf("expected mutation ID 1, got %d", rew.Mutations[0].ID)
	}

	// Exactly one rewritten source file should be present.
	if len(rew.Files) != 1 {
		t.Fatalf("expected 1 rewritten file, got %d", len(rew.Files))
	}

	for _, content := range rew.Files {
		if !strings.Contains(content, "__cMutInt(") {
			t.Errorf("rewritten file does not contain __cMutInt call:\n%s", content)
		}
		if strings.Contains(content, "a + b") {
			t.Errorf("rewritten file still contains bare a + b:\n%s", content)
		}
	}
}
