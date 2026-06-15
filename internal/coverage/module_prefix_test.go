package coverage

import (
	"reflect"
	"testing"
)

func TestParseModuleListHandlesWorkspaceAndNoise(t *testing.T) {
	t.Parallel()

	// go list -m prints one module per line in a workspace.
	got := parseModuleList("github.com/example/haystack\ngithub.com/example/haystack/tools\n")
	want := []string{"github.com/example/haystack", "github.com/example/haystack/tools"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseModuleList workspace = %v, want %v", got, want)
	}

	// Blank lines and "go:" toolchain notices must be ignored.
	got = parseModuleList("go: downloading go1.25.11\n\ngithub.com/example/mod\n")
	if !reflect.DeepEqual(got, []string{"github.com/example/mod"}) {
		t.Fatalf("parseModuleList noisy = %v", got)
	}

	if got := parseModuleList(""); got != nil {
		t.Fatalf("parseModuleList empty = %v, want nil", got)
	}
}

func TestStripModulePrefixWorkspaceLongestMatch(t *testing.T) {
	t.Parallel()

	c := Config{ModulePrefixes: normalizeModulePrefixes([]string{
		"github.com/example/haystack",
		"github.com/example/haystack/tools",
	})}

	cases := map[string]string{
		"github.com/example/haystack/searchcore/collection": "searchcore/collection",
		"github.com/example/haystack/tools/gen":             "gen", // longest prefix (tools/) wins
		"github.com/other/thing":                            "github.com/other/thing", // no match, unchanged
	}
	for in, want := range cases {
		if got := c.stripModulePrefix(in); got != want {
			t.Fatalf("stripModulePrefix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStripModulePrefixFallsBackToSinglePrefix(t *testing.T) {
	t.Parallel()

	// Back-compat: callers that set only ModulePrefix still get stripping.
	c := Config{ModulePrefix: "github.com/x/y/"}
	if got := c.stripModulePrefix("github.com/x/y/internal/a"); got != "internal/a" {
		t.Fatalf("stripModulePrefix single = %q", got)
	}

	// No prefix configured at all: input is returned unchanged.
	var empty Config
	if got := empty.stripModulePrefix("github.com/x/y/z"); got != "github.com/x/y/z" {
		t.Fatalf("stripModulePrefix no-prefix = %q", got)
	}
}

func TestParseFunctionCoverageOutputStripsWorkspacePrefixes(t *testing.T) {
	t.Parallel()

	input := `github.com/example/haystack/searchcore/collection/coll.go:10: New 80.0%
github.com/example/haystack/tools/gen.go:5: Gen 0.0%
total: (statements) 70.0%
`
	c := Config{ModulePrefixes: normalizeModulePrefixes([]string{
		"github.com/example/haystack",
		"github.com/example/haystack/tools",
	})}

	funcs := parseFunctionCoverageOutput(input, c)
	if len(funcs) != 2 {
		t.Fatalf("got %d funcs, want 2: %+v", len(funcs), funcs)
	}
	if funcs[0].Location != "searchcore/collection/coll.go:10:" {
		t.Fatalf("location[0] = %q", funcs[0].Location)
	}
	if funcs[1].Location != "gen.go:5:" {
		t.Fatalf("location[1] = %q", funcs[1].Location)
	}
}
