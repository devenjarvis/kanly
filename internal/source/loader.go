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
}

const loadMode = packages.NeedName |
	packages.NeedFiles |
	packages.NeedSyntax |
	packages.NeedTypes |
	packages.NeedTypesInfo |
	packages.NeedDeps |
	packages.NeedImports |
	packages.NeedCompiledGoFiles

func fromPackage(raw *packages.Package) (*Package, error) {
	if len(raw.Errors) > 0 {
		return nil, fmt.Errorf("package errors: %v", raw.Errors[0])
	}
	absDir := filepath.Dir(raw.Fset.File(raw.Syntax[0].Pos()).Name())
	files := make(map[string]*ast.File, len(raw.Syntax))
	for _, f := range raw.Syntax {
		pos := raw.Fset.Position(f.Pos())
		files[pos.Filename] = f
	}
	return &Package{
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

// LoadAll loads all packages matching the given patterns relative to workDir.
// If workDir is empty the current working directory is used.
// A single pattern that names an existing directory is treated as a direct
// package load (equivalent to Load), keeping single-package behaviour intact.
func LoadAll(workDir string, patterns ...string) ([]*Package, error) {
	if workDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("getwd: %w", err)
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
				return nil, fmt.Errorf("abs path: %w", err)
			}
			cfgDir = absPath
			loadPatterns = []string{"."}
		}
	}

	cfg := &packages.Config{Mode: loadMode, Dir: cfgDir, Tests: false}
	rawPkgs, err := packages.Load(cfg, loadPatterns...)
	if err != nil {
		return nil, fmt.Errorf("packages.Load: %w", err)
	}

	var result []*Package
	for _, raw := range rawPkgs {
		pkg, err := fromPackage(raw)
		if err != nil {
			return nil, err
		}
		result = append(result, pkg)
	}
	return result, nil
}
