package coverage

import "testing"

func TestParseFunctionCoverageOutputFiltersFilesAndFunctions(t *testing.T) {
	t.Parallel()

	input := `github.com/codetrek/haystack/internal/live.go:10: Run 81.2%
github.com/codetrek/haystack/internal/testutil/helper.go:11: Helper 0.0%
github.com/codetrek/haystack/internal/live.go:12: BuildForTest 0.0%
github.com/codetrek/haystack/internal/live.go:13: Replay 0.0%
total: (statements) 88.8%
`
	cfg := Config{
		ModulePrefix:        "github.com/codetrek/haystack/",
		ExcludeFiles:        []string{"internal/testutil/"},
		ExcludeFuncs:        []string{"Replay"},
		ExcludeFuncSuffixes: []string{"ForTest"},
	}

	funcs := parseFunctionCoverageOutput(input, cfg)
	if len(funcs) != 1 {
		t.Fatalf("got %d funcs, want 1: %+v", len(funcs), funcs)
	}
	if funcs[0].Location != "internal/live.go:10:" || funcs[0].Function != "Run" || funcs[0].Coverage != 81.2 {
		t.Fatalf("unexpected function coverage: %+v", funcs[0])
	}
}

func TestParseFunctionCoverageOutputKeepsSuffixWhenUnconfigured(t *testing.T) {
	t.Parallel()

	input := `github.com/codetrek/haystack/internal/live.go:10: Run 81.2%
github.com/codetrek/haystack/internal/live.go:12: BuildForTest 0.0%
total: (statements) 88.8%
`
	// With no suffixes configured the "ForTest" rule must not apply: the suffix
	// exclusion is now an explicit policy, not a baked-in default of the parser.
	cfg := Config{ModulePrefix: "github.com/codetrek/haystack/"}

	funcs := parseFunctionCoverageOutput(input, cfg)
	if len(funcs) != 2 {
		t.Fatalf("got %d funcs, want 2 (ForTest must not be filtered): %+v", len(funcs), funcs)
	}
}
