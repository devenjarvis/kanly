package runner

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// DirSnapshot records the files in a package directory, so that changes
// that mutants make to the user's checkout can be found and undone. Mutants
// run in the real source tree: a mutated path can make a test write into
// the package directory, or overwrite a fixture under testdata.
type DirSnapshot struct {
	dir   string
	files map[string]fileState
}

type fileState struct {
	size    int64
	modTime time.Time
	isDir   bool
}

// guardedPaths returns what SnapshotDir covers: the files directly in dir
// and everything under dir/testdata. Subdirectories are usually other
// packages, which walking would make expensive in a module root.
func guardedPaths(dir string) (map[string]fileState, error) {
	out := make(map[string]fileState)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue // removed while listing
		}
		path := filepath.Join(dir, e.Name())
		out[path] = fileState{size: info.Size(), modTime: info.ModTime(), isDir: e.IsDir()}
	}
	testdata := filepath.Join(dir, "testdata")
	err = filepath.WalkDir(testdata, func(path string, d fs.DirEntry, err error) error {
		if err != nil || path == testdata {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		out[path] = fileState{size: info.Size(), modTime: info.ModTime(), isDir: d.IsDir()}
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	return out, nil
}

// SnapshotDir records the files directly in dir and under dir/testdata.
func SnapshotDir(dir string) (*DirSnapshot, error) {
	files, err := guardedPaths(dir)
	if err != nil {
		return nil, err
	}
	return &DirSnapshot{dir: dir, files: files}, nil
}

// Restore removes the files and directories created since the snapshot and
// returns them, together with the snapshot's files that were since modified
// or deleted, which it cannot restore.
func (s *DirSnapshot) Restore() (removed, changed []string, err error) {
	now, err := guardedPaths(s.dir)
	if err != nil {
		return nil, nil, err
	}
	for path, st := range now {
		if _, existed := s.files[path]; existed {
			continue
		}
		parent := filepath.Dir(path)
		if _, parentListed := now[parent]; parentListed {
			if _, parentExisted := s.files[parent]; !parentExisted {
				continue // removed with its new parent directory
			}
		}
		if st.isDir {
			err = os.RemoveAll(path)
		} else {
			err = os.Remove(path)
		}
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return removed, changed, err
		}
		removed = append(removed, path)
	}
	for path, before := range s.files {
		after, ok := now[path]
		if !ok || (!before.isDir && (after.size != before.size || !after.modTime.Equal(before.modTime))) {
			changed = append(changed, path)
		}
	}
	sort.Strings(removed)
	sort.Strings(changed)
	return removed, changed, nil
}
