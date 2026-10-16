package schema

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// IgnoreFileName is the per-package file that accepts mutants by
// fingerprint, one per line with a required reason:
//
//	# comments and blank lines are skipped
//	3f9a1c2b7d40 equivalent: hunkRe re-checks the "@@" prefix
//
// It is for mutants that are judged rather than fixed: equivalent ones, and
// ones only a contrived test could kill. Unlike //kanly:ignore it keeps the
// judgement out of the code, and unlike an ID the fingerprint survives
// edits elsewhere in the package.
const IgnoreFileName = ".kanlyignore"

// fingerprintLen is the length, in hex digits, of a mutant fingerprint.
const fingerprintLen = 12

// fingerprint identifies a mutant by what it changes rather than by its ID
// or position: the enclosing function (scope), the operator, the original
// and mutant text, the whitespace-trimmed source line and the column within
// it. Edits elsewhere in the package renumber and move mutants but leave
// their fingerprints alone; an edit to the mutant's own line changes it,
// which is when a judgement that the mutant is equivalent must be made
// again. n numbers otherwise identical mutants in one scope, in ID order.
func fingerprint(scope, op, original, mutant, line string, col, n int) string {
	trimmed := strings.TrimLeft(line, " \t")
	col -= len(line) - len(trimmed)
	h := sha256.New()
	for _, s := range []string{scope, op, original, mutant, strings.TrimSpace(trimmed), strconv.Itoa(col), strconv.Itoa(n)} {
		h.Write([]byte(s))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:fingerprintLen]
}

// ReadIgnoreFile returns the fingerprints accepted by the IgnoreFileName
// file in dir, mapped to their reasons. A missing file accepts nothing. A
// malformed line, a missing reason or a repeated fingerprint is an error,
// so a typo cannot silently accept nothing.
func ReadIgnoreFile(dir string) (map[string]string, error) {
	path := filepath.Join(dir, IgnoreFileName)
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	accepted := make(map[string]string)
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if len(line) > 1 && line[0] == '#' && '0' <= line[1] && line[1] <= '9' {
			// "#42 reason" would otherwise be a comment that accepts nothing.
			return nil, fmt.Errorf("%s:%d: %q looks like a mutant ID; list the mutant's fingerprint (fp) instead, IDs change when the code does", path, n, line)
		}
		if line == "" || line[0] == '#' {
			continue
		}
		fp := line
		if i := strings.IndexAny(line, " \t"); i >= 0 {
			fp = line[:i]
		}
		reason := strings.TrimSpace(line[len(fp):])
		switch {
		case !isFingerprint(fp):
			return nil, fmt.Errorf("%s:%d: %q is not a mutant fingerprint (%d hex digits, printed with each mutant)", path, n, fp, fingerprintLen)
		case reason == "":
			return nil, fmt.Errorf("%s:%d: fingerprint %s needs a reason after it", path, n, fp)
		case accepted[fp] != "":
			return nil, fmt.Errorf("%s:%d: fingerprint %s is listed twice", path, n, fp)
		}
		accepted[fp] = reason
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return accepted, nil
}

func isFingerprint(s string) bool {
	if len(s) != fingerprintLen {
		return false
	}
	for _, c := range s {
		if !('0' <= c && c <= '9' || 'a' <= c && c <= 'f') {
			return false
		}
	}
	return true
}
