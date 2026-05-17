// Package diff parses `git diff` output into per-file line ranges suitable
// for filtering mutation candidates to only those touched by the current
// change set.
package diff

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// LineRange is a closed interval of line numbers on the post-change side of a hunk.
type LineRange struct {
	Start int
	End   int
}

// Diff is the parsed result of `git diff --unified=0 <base>`, keyed by absolute
// file path so callers can match against `Mutation.File` / `Package.Files` keys
// without any further normalisation.
type Diff struct {
	root   string
	byFile map[string][]LineRange
}

// hunkRe matches a unified-diff hunk header. We only care about the new-side
// start and (optional) count: `@@ -<a>[,<b>] +<c>[,<d>] @@ [context]`.
var hunkRe = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)

// Compute shells out to git and returns the diff between the working tree and base.
// workDir may be any directory inside the repo; paths in the result are absolute,
// rooted at the repo top-level (so subdirectory invocations still produce paths
// matching `Package.Files` keys).
func Compute(workDir, base string) (*Diff, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return nil, fmt.Errorf("git binary not found in PATH: %w", err)
	}

	rootCmd := exec.Command("git", "-C", workDir, "rev-parse", "--show-toplevel")
	rootOut, err := rootCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git rev-parse --show-toplevel (is %q inside a git repo?): %w", workDir, err)
	}
	root := strings.TrimSpace(string(rootOut))

	diffCmd := exec.Command("git", "-C", workDir, "diff", "--unified=0", base)
	var stderr bytes.Buffer
	diffCmd.Stderr = &stderr
	stdout, err := diffCmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := diffCmd.Start(); err != nil {
		return nil, fmt.Errorf("git diff: %w", err)
	}
	d, parseErr := ParseDiff(root, stdout)
	waitErr := diffCmd.Wait()
	if parseErr != nil {
		return nil, parseErr
	}
	if waitErr != nil {
		return nil, fmt.Errorf("git diff failed: %w: %s", waitErr, strings.TrimSpace(stderr.String()))
	}
	return d, nil
}

// ParseDiff parses unified-diff output (as produced by `git diff --unified=0`)
// into a Diff. root is prepended to each repo-relative path so the resulting
// keys are absolute. Exported so unit tests can exercise parsing without a real repo.
func ParseDiff(root string, r io.Reader) (*Diff, error) {
	d := &Diff{root: root, byFile: make(map[string][]LineRange)}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)

	currentFile := "" // empty means "skip lines until next file marker"
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "diff --git"):
			// New file block — reset until we see a +++ header.
			currentFile = ""
		case strings.HasPrefix(line, "+++ "):
			rest := strings.TrimPrefix(line, "+++ ")
			if i := strings.IndexByte(rest, '\t'); i >= 0 {
				rest = rest[:i]
			}
			if rest == "/dev/null" {
				currentFile = "" // file deletion — nothing to mutate
				continue
			}
			rest = strings.TrimPrefix(rest, "b/")
			currentFile = filepath.Join(root, filepath.FromSlash(rest))
		case strings.HasPrefix(line, "@@"):
			if currentFile == "" {
				continue
			}
			m := hunkRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			start, err := strconv.Atoi(m[1])
			if err != nil {
				continue
			}
			count := 1
			if m[2] != "" {
				count, err = strconv.Atoi(m[2])
				if err != nil {
					continue
				}
			}
			if count == 0 {
				continue // deletion-only hunk; no new lines to mutate
			}
			d.byFile[currentFile] = append(d.byFile[currentFile], LineRange{
				Start: start,
				End:   start + count - 1,
			})
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read diff: %w", err)
	}
	return d, nil
}

// Includes reports whether (absFile, line) falls within any changed range.
func (d *Diff) Includes(absFile string, line int) bool {
	for _, rng := range d.byFile[absFile] {
		if line >= rng.Start && line <= rng.End {
			return true
		}
	}
	return false
}

// Files returns the sorted set of absolute file paths touched by the diff.
func (d *Diff) Files() []string {
	out := make([]string, 0, len(d.byFile))
	for f := range d.byFile {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// Patterns returns deduped `./relative/dir` patterns suitable for
// `source.LoadAll(workDir, ...)`. Only directories containing at least one
// changed non-test .go file are included.
func (d *Diff) Patterns(workDir string) []string {
	absWork, err := filepath.Abs(workDir)
	if err != nil {
		absWork = workDir
	}
	seen := make(map[string]struct{})
	for f := range d.byFile {
		if !strings.HasSuffix(f, ".go") || strings.HasSuffix(f, "_test.go") {
			continue
		}
		dir := filepath.Dir(f)
		rel, err := filepath.Rel(absWork, dir)
		if err != nil {
			continue
		}
		var pat string
		if rel == "." {
			pat = "."
		} else {
			pat = "./" + filepath.ToSlash(rel)
		}
		seen[pat] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
