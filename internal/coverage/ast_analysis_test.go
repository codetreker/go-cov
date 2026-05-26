package coverage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAnalyzeBlockWithNoCovAnnotation(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "sample.go")
	src := `package sample

func Untested() {
	println("skip") // nocov
}
`
	if err := os.WriteFile(file, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	block := MergedBlock{File: file, StartLine: 3, StartCol: 1, EndLine: 5, EndCol: 1}
	AnalyzeBlockWithAST(&block, NewASTCache(), NewFileCache())

	if block.Level != "EXCLUDED" || block.FixAction != "nocov" || !block.NoCov {
		t.Fatalf("block was not excluded by nocov: %+v", block)
	}
}
