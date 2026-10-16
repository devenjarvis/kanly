package schema_test

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/devenjarvis/kanly/internal/mutation"
	"github.com/devenjarvis/kanly/internal/operators"
	"github.com/devenjarvis/kanly/internal/schema"
	"github.com/devenjarvis/kanly/internal/source"
)

const fpSource = `package fp

func Add(a, b int) int {
	return a + b
}

func Twice(a, b int) int {
	x := a + b
	x = x * 2
	x = x * 2
	return x
}
`

// rewriteSource writes src as a one-file module and rewrites it with
// int_arith, returning the mutations keyed by "Func line-text #n".
func rewriteSource(t *testing.T, src string, opts schema.Options) (*schema.Rewritten, map[string]mutation.Mutation) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/fp\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fp.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	pkg, err := source.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	rew, err := schema.RewriteWith(pkg, []mutation.Operator{operators.IntArith{}}, opts)
	if err != nil {
		t.Fatalf("RewriteWith: %v", err)
	}
	lines := strings.Split(src, "\n")
	byKey := make(map[string]mutation.Mutation)
	for _, m := range rew.Mutations {
		for n := 1; ; n++ {
			k := fmt.Sprintf("%s %s #%d", m.Function, strings.TrimSpace(lines[m.Line-1]), n)
			if _, dup := byKey[k]; !dup {
				byKey[k] = m
				break
			}
		}
	}
	return rew, byKey
}

func TestFingerprints(t *testing.T) {
	_, before := rewriteSource(t, fpSource, schema.Options{})
	seen := make(map[string]string)
	for k, m := range before {
		if len(m.Fingerprint) != 12 {
			t.Errorf("%s: fingerprint %q is not 12 hex digits", k, m.Fingerprint)
		}
		if other, dup := seen[m.Fingerprint]; dup {
			t.Errorf("%s and %s share fingerprint %s", k, other, m.Fingerprint)
		}
		seen[m.Fingerprint] = k
	}

	// A new function above and re-indented code renumber and move every
	// mutant, but leave the fingerprints alone.
	shifted := strings.Replace(fpSource, "func Add", "func Neg(a int) int { return 0 - a }\n\nfunc Add", 1)
	shifted = strings.ReplaceAll(shifted, "\t", "    ")
	_, after := rewriteSource(t, shifted, schema.Options{})
	for k, m := range before {
		a, ok := after[k]
		if !ok {
			t.Fatalf("%s missing after the edit", k)
		}
		if a.ID == m.ID || a.Line == m.Line {
			t.Errorf("%s: the edit was meant to move it (ID %d→%d, line %d→%d)", k, m.ID, a.ID, m.Line, a.Line)
		}
		if a.Fingerprint != m.Fingerprint {
			t.Errorf("%s: fingerprint changed from %s to %s by an edit elsewhere", k, m.Fingerprint, a.Fingerprint)
		}
	}

	// Editing the mutated line itself changes the fingerprint.
	_, edited := rewriteSource(t, strings.Replace(fpSource, "return a + b", "return b + a", 1), schema.Options{})
	if got := edited["Add return b + a #1"].Fingerprint; got == before["Add return a + b #1"].Fingerprint {
		t.Errorf("editing the mutated line kept fingerprint %s", got)
	}
}

func TestRewriteAccepted(t *testing.T) {
	_, all := rewriteSource(t, fpSource, schema.Options{})
	add, twice := all["Add return a + b #1"], all["Twice x := a + b #1"]

	const bogus = "0123456789ab"
	rew, kept := rewriteSource(t, fpSource, schema.Options{
		Accepted: map[string]string{add.Fingerprint: "equivalent", twice.Fingerprint: "not worth it", bogus: "gone"},
		// Twice is out of scope: its entry must not be reported stale.
		Filter: func(_ string, _ int, fn string) bool { return fn == "Add" },
	})
	if len(kept) != 0 {
		t.Errorf("kept %v, want nothing (Add's mutant is accepted, Twice is filtered)", kept)
	}
	if rew.Ignored != 1 {
		t.Errorf("Ignored = %d, want 1", rew.Ignored)
	}
	if !reflect.DeepEqual(rew.StaleAccepted, []string{bogus}) {
		t.Errorf("StaleAccepted = %v, want [%s]", rew.StaleAccepted, bogus)
	}
}

func TestReadIgnoreFile(t *testing.T) {
	tests := []struct {
		name    string
		content string // "" means no file
		want    map[string]string
		wantErr string
	}{
		{name: "missing file"},
		{
			name:    "entries, comments and blank lines",
			content: "# accepted mutants\n\n0123456789ab equivalent: re-checked below\nabcdef012345\tonly a 64KB line kills it  \n",
			want:    map[string]string{"0123456789ab": "equivalent: re-checked below", "abcdef012345": "only a 64KB line kills it"},
		},
		{name: "missing reason", content: "0123456789ab\n", wantErr: "needs a reason"},
		{name: "mutant ID", content: "#42 equivalent\n", wantErr: "looks like a mutant ID"},
		{name: "bad fingerprint", content: "42 equivalent\n", wantErr: "not a mutant fingerprint"},
		{name: "upper-case hex", content: "0123456789AB equivalent\n", wantErr: "not a mutant fingerprint"},
		{name: "duplicate", content: "0123456789ab one\n0123456789ab two\n", wantErr: "listed twice"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if tt.content != "" {
				if err := os.WriteFile(filepath.Join(dir, schema.IgnoreFileName), []byte(tt.content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			got, err := schema.ReadIgnoreFile(dir)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want one containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tt.want) || (len(tt.want) > 0 && !reflect.DeepEqual(got, tt.want)) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
