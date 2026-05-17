package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/devenjarvis/kanly/internal/schema"
)

type overlayJSON struct {
	Replace map[string]string `json:"Replace"`
}

// BuildOverlay writes rewritten files and the dispatcher to a temp dir, creates an overlay JSON,
// and returns the path to that JSON plus a cleanup function.
func BuildOverlay(rew *schema.Rewritten, pkgDir string) (overlayPath string, cleanup func(), err error) {
	tmpDir, err := os.MkdirTemp("", "kanly-overlay-*")
	if err != nil {
		return "", nil, fmt.Errorf("create temp dir: %w", err)
	}

	cleanup = func() { os.RemoveAll(tmpDir) }
	defer func() {
		if err != nil {
			os.RemoveAll(tmpDir)
		}
	}()

	replace := make(map[string]string)

	// Write each rewritten source file to the temp dir.
	// Use an index prefix to avoid collisions when multiple files share the same base name.
	i := 0
	for origPath, content := range rew.Files {
		absOrig, err := filepath.Abs(origPath)
		if err != nil {
			return "", nil, fmt.Errorf("abs path for %s: %w", origPath, err)
		}

		tmpFile := filepath.Join(tmpDir, fmt.Sprintf("%04d_%s", i, filepath.Base(origPath)))
		if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
			return "", nil, fmt.Errorf("write rewritten file: %w", err)
		}
		replace[absOrig] = tmpFile
		i++
	}

	// Write the dispatcher file (kanly_schema.go) into the package directory overlay slot.
	dispatcherDst := filepath.Join(tmpDir, "kanly_schema.go")
	if err := os.WriteFile(dispatcherDst, []byte(rew.Dispatcher), 0644); err != nil {
		return "", nil, fmt.Errorf("write dispatcher: %w", err)
	}

	// The dispatcher is a new file that doesn't exist in the original package.
	// The overlay key must be the absolute path it would have if it were in the package dir.
	absDispatcher := filepath.Join(pkgDir, "kanly_schema.go")
	replace[absDispatcher] = dispatcherDst

	overlay := overlayJSON{Replace: replace}
	data, err := json.Marshal(overlay)
	if err != nil {
		return "", nil, fmt.Errorf("marshal overlay: %w", err)
	}

	overlayFile := filepath.Join(tmpDir, "overlay.json")
	if err := os.WriteFile(overlayFile, data, 0644); err != nil {
		return "", nil, fmt.Errorf("write overlay.json: %w", err)
	}

	return overlayFile, cleanup, nil
}
