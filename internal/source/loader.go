package source

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"
)

type Package struct {
	Dir        string
	ImportPath string
	Files      map[string]*ast.File
	Fset       *token.FileSet
	TypesInfo  *types.Info
	Pkg        *types.Package
	// GoVersion is the `go` directive of the package's module (e.g. "1.16"),
	// or "" when the package is not part of a module. A module without a
	// `go` directive reports "1.16", matching the go command's default.
	GoVersion string
}

const loadMode = packages.NeedName |
	packages.NeedFiles |
	packages.NeedSyntax |
	packages.NeedTypes |
	packages.NeedTypesInfo |
	packages.NeedDeps |
	packages.NeedImports |
	packages.NeedCompiledGoFiles |
	packages.NeedModule

func fromPackage(raw *packages.Package) (*Package, error) {
	if len(raw.Errors) > 0 {
		return nil, fmt.Errorf("package errors: %v", raw.Errors[0])
	}
	// Packages with no non-test source files (e.g. directories that contain
	// only _test.go files with an external test package name) appear in ./...
	// expansion but have nothing to mutate — skip them silently.
	if len(raw.Syntax) == 0 {
		return nil, nil
	}
	absDir := filepath.Dir(raw.Fset.File(raw.Syntax[0].Pos()).Name())
	files := make(map[string]*ast.File, len(raw.Syntax))
	for _, f := range raw.Syntax {
		pos := raw.Fset.Position(f.Pos())
		files[pos.Filename] = f
	}
	goVersion := ""
	if raw.Module != nil {
		goVersion = raw.Module.GoVersion
		if goVersion == "" {
			goVersion = "1.16"
		}
	}
	return &Package{
		GoVersion:  goVersion,
		Dir:        absDir,
		ImportPath: raw.PkgPath,
		Files:      files,
		Fset:       raw.Fset,
		TypesInfo:  raw.TypesInfo,
		Pkg:        raw.Types,
	}, nil
}

func Load(dir string) (*Package, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("abs dir: %w", err)
	}
	cfg := &packages.Config{Mode: loadMode, Dir: absDir, Tests: false}

	pkgs, err := packages.Load(cfg, ".")
	if err != nil {
		return nil, fmt.Errorf("packages.Load: %w", err)
	}
	if len(pkgs) != 1 {
		return nil, fmt.Errorf("expected 1 package, got %d", len(pkgs))
	}

	return fromPackage(pkgs[0])
}

// LoadError reports a matched package that could not be loaded, for example
// because it does not type-check.
type LoadError struct {
	ImportPath string
	Err        error
}

// LoadAll loads all packages matching the given patterns relative to workDir.
// If workDir is empty the current working directory is used.
// A single pattern that names an existing directory is treated as a direct
// package load (equivalent to Load), keeping single-package behaviour intact.
//
// A package that fails to load is returned in the LoadError slice rather than
// failing the whole load, so callers can still process its healthy siblings.
// The error result is reserved for failures of the load itself.
func LoadAll(workDir string, patterns ...string) ([]*Package, []LoadError, error) {
	return LoadAllTags(workDir, "", patterns...)
}

// LoadAllTags is LoadAll with build tags, a comma-separated list as for
// `go build -tags`.
func LoadAllTags(workDir, tags string, patterns ...string) ([]*Package, []LoadError, error) {
	if workDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, nil, fmt.Errorf("getwd: %w", err)
		}
		workDir = cwd
	}

	cfgDir := workDir
	loadPatterns := patterns

	// Single non-glob argument that is a directory → mimic Load behaviour.
	if len(patterns) == 1 && !strings.Contains(patterns[0], "...") {
		if info, err := os.Stat(patterns[0]); err == nil && info.IsDir() {
			absPath, err := filepath.Abs(patterns[0])
			if err != nil {
				return nil, nil, fmt.Errorf("abs path: %w", err)
			}
			cfgDir = absPath
			loadPatterns = []string{"."}
		}
	}

	cfg := &packages.Config{Mode: loadMode, Dir: cfgDir, Tests: false}
	if tags != "" {
		cfg.BuildFlags = []string{"-tags=" + tags}
	}
	rawPkgs, err := packages.Load(cfg, loadPatterns...)
	if err != nil {
		return nil, nil, fmt.Errorf("packages.Load: %w", err)
	}

	var result []*Package
	var loadErrs []LoadError
	for _, raw := range rawPkgs {
		pkg, err := fromPackage(raw)
		if err != nil {
			path := raw.PkgPath
			if path == "" {
				path = raw.ID
			}
			loadErrs = append(loadErrs, LoadError{ImportPath: path, Err: err})
			continue
		}
		if pkg == nil {
			continue // test-only package, nothing to mutate
		}
		result = append(result, pkg)
	}
	return result, loadErrs, nil
}
