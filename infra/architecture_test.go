package infra_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Reusable infrastructure must remain independently usable by other layers.
func TestInternalImportBoundaries(t *testing.T) {
	err := filepath.WalkDir("../internal", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range file.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if strings.HasPrefix(importPath, "LyricsPipeline/infra") || strings.HasPrefix(importPath, "LyricsPipeline/transcription") {
				t.Errorf("%s imports application dependency %s", path, importPath)
			}
			if filepath.Base(filepath.Dir(path)) == "lambdabuild" && strings.Contains(strings.Split(importPath, "/")[0], ".") {
				t.Errorf("builder must use only standard library: %s", importPath)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
