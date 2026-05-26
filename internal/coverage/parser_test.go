package coverage

import "testing"

func TestParseLineTrimsModulePrefix(t *testing.T) {
	t.Parallel()

	cfg := Config{ModulePrefix: "github.com/codetrek/haystack/"}
	block, ok := parseLine("github.com/codetrek/haystack/internal/foo.go:10.2,12.1 3 0", cfg)
	if !ok {
		t.Fatal("parseLine() returned ok=false")
	}

	if block.File != "internal/foo.go" {
		t.Fatalf("block.File = %q, want trimmed path", block.File)
	}
	if block.StartLine != 10 || block.StartCol != 2 || block.EndLine != 12 || block.EndCol != 1 || block.Count != 0 {
		t.Fatalf("unexpected block: %+v", block)
	}
}

func TestParseLineSkipsExcludedFile(t *testing.T) {
	t.Parallel()

	cfg := Config{
		ModulePrefix: "github.com/codetrek/haystack/",
		ExcludeFiles: []string{"internal/testutil/"},
	}

	_, ok := parseLine("github.com/codetrek/haystack/internal/testutil/helper.go:1.1,2.1 1 0", cfg)
	if ok {
		t.Fatal("parseLine() included an excluded file")
	}
}
