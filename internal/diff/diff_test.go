package diff_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/devenjarvis/kanly/internal/diff"
)

func TestParseDiff(t *testing.T) {
	root := "/repo"

	type want struct {
		file   string
		ranges []diff.LineRange
	}

	cases := []struct {
		name  string
		input string
		want  []want
	}{
		{
			name: "single hunk with count",
			input: `diff --git a/foo.go b/foo.go
index 0..1 100644
--- a/foo.go
+++ b/foo.go
@@ -10,2 +11,3 @@ func F() {
+a
+b
+c
`,
			want: []want{{file: filepath.Join(root, "foo.go"), ranges: []diff.LineRange{{Start: 11, End: 13}}}},
		},
		{
			name: "hunk without count means single line",
			input: `diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
@@ -5 +5 @@
-old
+new
`,
			want: []want{{file: filepath.Join(root, "foo.go"), ranges: []diff.LineRange{{Start: 5, End: 5}}}},
		},
		{
			name: "deletion-only hunk yields no range",
			input: `diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
@@ -3,2 +2,0 @@
-removed1
-removed2
`,
			want: nil,
		},
		{
			name: "multi-file diff",
			input: `diff --git a/a.go b/a.go
--- a/a.go
+++ b/a.go
@@ -1 +1 @@
-x
+y
diff --git a/b/c.go b/b/c.go
--- a/b/c.go
+++ b/b/c.go
@@ -10,0 +11,2 @@
+p
+q
`,
			want: []want{
				{file: filepath.Join(root, "a.go"), ranges: []diff.LineRange{{Start: 1, End: 1}}},
				{file: filepath.Join(root, "b", "c.go"), ranges: []diff.LineRange{{Start: 11, End: 12}}},
			},
		},
		{
			name: "deleted file is skipped",
			input: `diff --git a/gone.go b/gone.go
deleted file mode 100644
--- a/gone.go
+++ /dev/null
@@ -1,3 +0,0 @@
-a
-b
-c
`,
			want: nil,
		},
		{
			name: "binary files skipped",
			input: `diff --git a/img.png b/img.png
Binary files a/img.png and b/img.png differ
`,
			want: nil,
		},
		{
			name: "rename keyed under new path",
			input: `diff --git a/old.go b/new.go
similarity index 80%
rename from old.go
rename to new.go
--- a/old.go
+++ b/new.go
@@ -1 +1 @@
-old line
+new line
`,
			want: []want{{file: filepath.Join(root, "new.go"), ranges: []diff.LineRange{{Start: 1, End: 1}}}},
		},
		{
			name: "multiple hunks in one file",
			input: `diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
@@ -5 +5 @@
-x
+y
@@ -20,0 +21,2 @@
+a
+b
`,
			want: []want{{file: filepath.Join(root, "foo.go"), ranges: []diff.LineRange{{Start: 5, End: 5}, {Start: 21, End: 22}}}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, err := diff.ParseDiff(root, strings.NewReader(tc.input))
			if err != nil {
				t.Fatalf("ParseDiff: %v", err)
			}
			for _, w := range tc.want {
				for _, r := range w.ranges {
					for line := r.Start; line <= r.End; line++ {
						if !d.Includes(w.file, line) {
							t.Errorf("Includes(%s, %d) = false, want true", w.file, line)
						}
					}
				}
			}
			gotFiles := d.Files()
			wantFiles := make([]string, 0, len(tc.want))
			for _, w := range tc.want {
				wantFiles = append(wantFiles, w.file)
			}
			if !reflect.DeepEqual(gotFiles, wantFiles) {
				t.Errorf("Files() = %v, want %v", gotFiles, wantFiles)
			}
		})
	}
}

func TestIncludesNotInDiff(t *testing.T) {
	d, err := diff.ParseDiff("/repo", strings.NewReader(`diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
@@ -5 +5 @@
-x
+y
`))
	if err != nil {
		t.Fatal(err)
	}
	if d.Includes("/repo/foo.go", 4) {
		t.Errorf("line 4 should not be included")
	}
	if d.Includes("/repo/foo.go", 6) {
		t.Errorf("line 6 should not be included")
	}
	if d.Includes("/repo/other.go", 5) {
		t.Errorf("untouched file should not be included")
	}
}

func TestPatternsExcludesTestFiles(t *testing.T) {
	input := `diff --git a/pkg/a.go b/pkg/a.go
--- a/pkg/a.go
+++ b/pkg/a.go
@@ -1 +1 @@
-x
+y
diff --git a/pkg/a_test.go b/pkg/a_test.go
--- a/pkg/a_test.go
+++ b/pkg/a_test.go
@@ -1 +1 @@
-x
+y
diff --git a/other/b.go b/other/b.go
--- a/other/b.go
+++ b/other/b.go
@@ -1 +1 @@
-x
+y
`
	tmp := t.TempDir()
	d, err := diff.ParseDiff(tmp, strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}

	got := d.Patterns(tmp)
	want := []string{"./other", "./pkg"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Patterns() = %v, want %v", got, want)
	}
}

func TestPatternsOnlyTestFiles(t *testing.T) {
	input := `diff --git a/pkg/a_test.go b/pkg/a_test.go
--- a/pkg/a_test.go
+++ b/pkg/a_test.go
@@ -1 +1 @@
-x
+y
`
	tmp := t.TempDir()
	d, err := diff.ParseDiff(tmp, strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if got := d.Patterns(tmp); len(got) != 0 {
		t.Errorf("Patterns() = %v, want empty (test-only diff)", got)
	}
}

// TestComputeIntegration spins up a tmp git repo, makes an edit, and asserts
// Compute returns the changed line range. Gated by -short because it shells
// out to git.
func TestComputeIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping git integration test in short mode")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available")
	}

	repo := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=t@e.x",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=t@e.x",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "commit.gpgsign", "false")

	filePath := filepath.Join(repo, "foo.go")
	original := "package foo\n\nfunc Add(a, b int) int { return a + b }\n"
	if err := os.WriteFile(filePath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "foo.go")
	run("commit", "-q", "-m", "init")

	// Modify line 3.
	modified := "package foo\n\nfunc Add(a, b int) int { return a - b }\n"
	if err := os.WriteFile(filePath, []byte(modified), 0o644); err != nil {
		t.Fatal(err)
	}

	d, err := diff.Compute(repo, "HEAD")
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}

	abs := filepath.Join(repo, "foo.go")
	if !d.Includes(abs, 3) {
		t.Errorf("Includes(%s, 3) = false, want true", abs)
	}
	if d.Includes(abs, 1) {
		t.Errorf("Includes(%s, 1) = true, want false", abs)
	}

	files := d.Files()
	if len(files) != 1 || files[0] != abs {
		t.Errorf("Files() = %v, want [%s]", files, abs)
	}

	patterns := d.Patterns(repo)
	if !reflect.DeepEqual(patterns, []string{"."}) {
		t.Errorf("Patterns() = %v, want [\".\"]", patterns)
	}
}
