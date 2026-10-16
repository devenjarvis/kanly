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

// Restored reports what DirSnapshot.Restore found.
type Restored struct {
	Moved      []string // created during the run, moved to Quarantine
	Quarantine string   // directory holding Moved, keeping their relative paths
	Unmoved    []string // created during the run but could not be moved
	Changed    []string // existed before and were modified or deleted
}

// Restore moves the files and directories created since the snapshot into
// a new quarantine directory under os.TempDir, keeping their paths relative
// to the snapshot's directory. Moving rather than deleting keeps a file the
// user created during the run recoverable. A file that cannot be moved (the
// temporary directory may be on another file system) is left in place and
// reported. Files that existed before and were since modified or deleted
// cannot be restored; they are reported too.
func (s *DirSnapshot) Restore() (Restored, error) {
	var r Restored
	now, err := guardedPaths(s.dir)
	if err != nil {
		return r, err
	}
	var created []string
	for path := range now {
		if _, existed := s.files[path]; existed {
			continue
		}
		parent := filepath.Dir(path)
		if _, parentListed := now[parent]; parentListed {
			if _, parentExisted := s.files[parent]; !parentExisted {
				continue // moved with its new parent directory
			}
		}
		created = append(created, path)
	}
	sort.Strings(created)
	for _, path := range created {
		if err := s.quarantine(&r, path); err != nil {
			if !errors.Is(err, fs.ErrNotExist) { // gone since it was listed
				r.Unmoved = append(r.Unmoved, path)
			}
			continue
		}
		r.Moved = append(r.Moved, path)
	}
	for path, before := range s.files {
		after, ok := now[path]
		if !ok || (!before.isDir && (after.size != before.size || !after.modTime.Equal(before.modTime))) {
			r.Changed = append(r.Changed, path)
		}
	}
	sort.Strings(r.Changed)
	return r, nil
}

// quarantine moves path into r.Quarantine, creating that directory first.
func (s *DirSnapshot) quarantine(r *Restored, path string) error {
	if r.Quarantine == "" {
		dir, err := os.MkdirTemp("", "kanly-moved-*")
		if err != nil {
			return err
		}
		r.Quarantine = dir
	}
	rel, err := filepath.Rel(s.dir, path)
	if err != nil {
		return err
	}
	dst := filepath.Join(r.Quarantine, rel)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.Rename(path, dst)
}
