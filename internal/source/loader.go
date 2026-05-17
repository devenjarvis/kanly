package source

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"

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

func Load(dir string) (*Package, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("abs dir: %w", err)
	}
	cfg := &packages.Config{
		Mode: packages.NeedName |
			packages.NeedFiles |
			packages.NeedSyntax |
			packages.NeedTypes |
			packages.NeedTypesInfo |
			packages.NeedDeps |
			packages.NeedImports |
			packages.NeedCompiledGoFiles,
		Dir:   absDir,
		Tests: false,
	}

	pkgs, err := packages.Load(cfg, ".")
	if err != nil {
		return nil, fmt.Errorf("packages.Load: %w", err)
	}
	if len(pkgs) != 1 {
		return nil, fmt.Errorf("expected 1 package, got %d", len(pkgs))
	}

	pkg := pkgs[0]
	if len(pkg.Errors) > 0 {
		return nil, fmt.Errorf("package errors: %v", pkg.Errors[0])
	}

	files := make(map[string]*ast.File, len(pkg.Syntax))
	for _, f := range pkg.Syntax {
		pos := pkg.Fset.Position(f.Pos())
		files[pos.Filename] = f
	}

	return &Package{
		Dir:        absDir,
		ImportPath: pkg.PkgPath,
		Files:      files,
		Fset:       pkg.Fset,
		TypesInfo:  pkg.TypesInfo,
		Pkg:        pkg.Types,
	}, nil
}
