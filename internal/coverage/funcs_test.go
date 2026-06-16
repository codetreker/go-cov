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

func TestParseTotalCoverageOutput(t *testing.T) {
	t.Parallel()

	// A representative `go tool cover -func` output: per-function lines followed
	// by the trailing "total:" line that this helper must extract.
	input := `github.com/codetrek/haystack/internal/live.go:10: Run 81.2%
github.com/codetrek/haystack/internal/live.go:13: Replay 0.0%
total:							(statements)	88.8%
`
	got, known := parseTotalCoverageOutput(input)
	if !known {
		t.Fatalf("parseTotalCoverageOutput() known = false, want true when total line present")
	}
	if got != 88.8 {
		t.Fatalf("parseTotalCoverageOutput() = %v, want 88.8", got)
	}
}

func TestParseTotalCoverageOutputMissingTotal(t *testing.T) {
	t.Parallel()

	// No "total:" line: this is "no data", not a genuine 0% coverage. The helper
	// must report the total as unknown so the caller does not treat it as 0/CRITICAL.
	input := `github.com/codetrek/haystack/internal/live.go:10: Run 81.2%
`
	got, known := parseTotalCoverageOutput(input)
	if known {
		t.Fatalf("parseTotalCoverageOutput() known = true, want false when no total line")
	}
	if got != 0 {
		t.Fatalf("parseTotalCoverageOutput() = %v, want 0 when no total line", got)
	}
}

func TestParseTotalCoverageOutputGenuineZero(t *testing.T) {
	t.Parallel()

	// A real total of 0.0% must be reported as known: it is genuine "0% covered"
	// data, distinct from an absent/unreadable total.
	input := `github.com/codetrek/haystack/internal/live.go:10: Run 0.0%
total:							(statements)	0.0%
`
	got, known := parseTotalCoverageOutput(input)
	if !known {
		t.Fatalf("parseTotalCoverageOutput() known = false, want true for a genuine 0.0%% total")
	}
	if got != 0 {
		t.Fatalf("parseTotalCoverageOutput() = %v, want 0", got)
	}
}
