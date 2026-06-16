package coverage

import (
	"bytes"
	"strings"
	"testing"
)

// The renderers below write through an injected io.Writer, so they can be
// exercised directly against a bytes.Buffer instead of being glued to stdout.

// A package at or above threshold renders a plain "ok" row with its coverage and
// no CRITICAL marker.
func TestPrintPackageSummaryNormal(t *testing.T) {
	cfg := Config{ThresholdPackage: 85.0, ShowTestCounts: true}
	results := []PackageResult{
		{Name: "pkg/good", Status: "ok", Duration: "0.10s", Coverage: 92.5, CoverageStr: "92.5%"},
	}

	var buf bytes.Buffer
	hasCritical := printPackageSummary(&buf, cfg, results,
		map[string]int{"pkg/good": 4}, map[string]int{"pkg/good": 1})
	out := buf.String()

	if hasCritical {
		t.Fatalf("hasCritical = true for an above-threshold package:\n%s", out)
	}
	if !strings.Contains(out, "Package coverage summary:") {
		t.Fatalf("missing summary header:\n%s", out)
	}
	if !strings.Contains(out, "pkg/good") || !strings.Contains(out, "92.5%") {
		t.Fatalf("missing package row content:\n%s", out)
	}
	if strings.Contains(out, "CRITICAL") {
		t.Fatalf("unexpected CRITICAL marker for an above-threshold package:\n%s", out)
	}
}

// A below-threshold package must be flagged CRITICAL and return hasCritical=true.
func TestPrintPackageSummaryCritical(t *testing.T) {
	cfg := Config{ThresholdPackage: 85.0, ShowTestCounts: true}
	results := []PackageResult{
		{Name: "pkg/low", Status: "ok", Duration: "0.10s", Coverage: 40.0, CoverageStr: "40.0%"},
	}

	var buf bytes.Buffer
	hasCritical := printPackageSummary(&buf, cfg, results,
		map[string]int{"pkg/low": 1}, map[string]int{})
	out := buf.String()

	if !hasCritical {
		t.Fatalf("hasCritical = false for a below-threshold package:\n%s", out)
	}
	if !strings.Contains(out, "CRITICAL") || !strings.Contains(out, "pkg/low") {
		t.Fatalf("missing CRITICAL marker for below-threshold package:\n%s", out)
	}
}

// Function coverage rendering: a function at/above the function threshold but
// below the print threshold is listed without a CRITICAL marker.
func TestPrintFunctionCoverageNormal(t *testing.T) {
	cfg := Config{ThresholdFunc: 80.0, ThresholdPrint: 90.0}
	funcs := []FuncCoverage{
		{Location: "pkg/file.go:10:", Function: "Below", Coverage: 85.0},
	}

	var buf bytes.Buffer
	hasCritical, width := printFunctionCoverage(&buf, cfg, funcs)
	out := buf.String()

	if hasCritical {
		t.Fatalf("hasCritical = true for an above-func-threshold function:\n%s", out)
	}
	if width <= 0 {
		t.Fatalf("expected a positive alignment width, got %d", width)
	}
	if !strings.Contains(out, "Below") || !strings.Contains(out, "85.0%") {
		t.Fatalf("missing function row content:\n%s", out)
	}
	if strings.Contains(out, "CRITICAL") {
		t.Fatalf("unexpected CRITICAL marker for an above-func-threshold function:\n%s", out)
	}
}

// A function below the function threshold must be flagged CRITICAL.
func TestPrintFunctionCoverageCritical(t *testing.T) {
	cfg := Config{ThresholdFunc: 80.0, ThresholdPrint: 90.0}
	funcs := []FuncCoverage{
		{Location: "pkg/file.go:20:", Function: "Weak", Coverage: 10.0},
	}

	var buf bytes.Buffer
	hasCritical, _ := printFunctionCoverage(&buf, cfg, funcs)
	out := buf.String()

	if !hasCritical {
		t.Fatalf("hasCritical = false for a below-func-threshold function:\n%s", out)
	}
	if !strings.Contains(out, "Weak") || !strings.Contains(out, "CRITICAL") {
		t.Fatalf("missing CRITICAL marker for below-func-threshold function:\n%s", out)
	}
}

// In CI mode a critical function must emit a GitHub Actions error annotation
// pinned to its file and line.
func TestPrintFunctionCoverageCriticalCIAnnotation(t *testing.T) {
	cfg := Config{ThresholdFunc: 80.0, ThresholdPrint: 90.0, CIMode: true}
	funcs := []FuncCoverage{
		{Location: "pkg/file.go:20:", Function: "Weak", Coverage: 10.0},
	}

	var buf bytes.Buffer
	printFunctionCoverage(&buf, cfg, funcs)
	out := buf.String()

	if !strings.Contains(out, "::error file=pkg/file.go,line=20::") {
		t.Fatalf("missing CI error annotation for critical function:\n%s", out)
	}
}

// A total at/above threshold renders the percentage without a CRITICAL marker
// and returns false.
func TestPrintTotalNormal(t *testing.T) {
	cfg := Config{ThresholdTotal: 90.0}

	var buf bytes.Buffer
	isCritical := printTotal(&buf, cfg, 93.4, true, 60)
	out := buf.String()

	if isCritical {
		t.Fatalf("isCritical = true for an above-threshold total:\n%s", out)
	}
	if !strings.Contains(out, "TOTAL") || !strings.Contains(out, "93.4%") {
		t.Fatalf("missing total row content:\n%s", out)
	}
	if strings.Contains(out, "CRITICAL") {
		t.Fatalf("unexpected CRITICAL marker for an above-threshold total:\n%s", out)
	}
}

// A below-threshold total must be flagged CRITICAL and return true.
func TestPrintTotalCritical(t *testing.T) {
	cfg := Config{ThresholdTotal: 90.0}

	var buf bytes.Buffer
	isCritical := printTotal(&buf, cfg, 50.0, true, 60)
	out := buf.String()

	if !isCritical {
		t.Fatalf("isCritical = false for a below-threshold total:\n%s", out)
	}
	if !strings.Contains(out, "TOTAL") || !strings.Contains(out, "CRITICAL") {
		t.Fatalf("missing CRITICAL marker for below-threshold total:\n%s", out)
	}
}

// An unknown total ("no data") renders "n/a" and is never CRITICAL.
func TestPrintTotalUnknown(t *testing.T) {
	cfg := Config{ThresholdTotal: 90.0}

	var buf bytes.Buffer
	isCritical := printTotal(&buf, cfg, 0, false, 60)
	out := buf.String()

	if isCritical {
		t.Fatalf("isCritical = true for an unknown total:\n%s", out)
	}
	if !strings.Contains(out, "n/a") {
		t.Fatalf("expected n/a for an unknown total:\n%s", out)
	}
}

// Statistics rendering buckets functions by coverage band and reports the test
// totals when ShowTestCounts is enabled.
func TestPrintStatistics(t *testing.T) {
	cfg := Config{ShowTestCounts: true}
	funcs := []FuncCoverage{
		{Function: "a", Coverage: 100.0},
		{Function: "b", Coverage: 97.0},
		{Function: "c", Coverage: 88.0},
		{Function: "d", Coverage: 50.0},
	}

	var buf bytes.Buffer
	printStatistics(&buf, cfg, funcs,
		map[string]int{"pkg": 3}, map[string]int{"pkg": 2})
	out := buf.String()

	for _, want := range []string{
		"Statistics:",
		"Functions with 100% coverage: 1",
		"Functions with 95%-100% coverage: 1",
		"Functions with 85%-95% coverage: 1",
		"Functions with <85% coverage: 1",
		"Total tests: 5 (including subtests, 3 top-level)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("statistics output missing %q:\n%s", want, out)
		}
	}
}

// With ShowTestCounts disabled the test-total line is omitted.
func TestPrintStatisticsOmitsTestTotalsWhenDisabled(t *testing.T) {
	cfg := Config{ShowTestCounts: false}

	var buf bytes.Buffer
	printStatistics(&buf, cfg, nil, map[string]int{"pkg": 3}, map[string]int{})
	out := buf.String()

	if strings.Contains(out, "Total tests:") {
		t.Fatalf("test totals leaked when ShowTestCounts is false:\n%s", out)
	}
}

// printBlocks renders printable (non-LOW) blocks; LOW blocks are omitted and a
// "... N more ..." tail is shown when the limit elides some non-critical entries.
// CRITICAL blocks are always rendered regardless of the limit.
func TestPrintBlocks(t *testing.T) {
	merged := []MergedBlock{
		{File: "a.go", StartLine: 1, StartCol: 1, EndLine: 3, EndCol: 1,
			NumLines: 2, EffectiveLines: 2, Level: "CRITICAL", FixAction: "Required"},
		{File: "b.go", StartLine: 5, StartCol: 1, EndLine: 6, EndCol: 1,
			NumLines: 1, EffectiveLines: 1, Level: "HIGH", FixAction: "Recommended"},
		{File: "c.go", StartLine: 8, StartCol: 1, EndLine: 9, EndCol: 1,
			NumLines: 1, EffectiveLines: 1, Level: "HIGH", FixAction: "Recommended"},
		{File: "low.go", StartLine: 11, StartCol: 1, EndLine: 12, EndCol: 1,
			NumLines: 1, EffectiveLines: 1, Level: "LOW", FixAction: ""},
	}

	// limit=1: CRITICAL always prints, one non-critical prints, the remaining
	// non-critical block is elided into the "more" tail, and the LOW block is
	// omitted entirely (and not counted).
	var buf bytes.Buffer
	printBlocks(&buf, merged, 20, 1, false, false)
	out := buf.String()

	if !strings.Contains(out, "a.go") || !strings.Contains(out, "CRITICAL") {
		t.Fatalf("missing CRITICAL block in output:\n%s", out)
	}
	if !strings.Contains(out, "b.go") {
		t.Fatalf("expected the first non-critical block to render:\n%s", out)
	}
	if strings.Contains(out, "c.go") {
		t.Fatalf("non-critical block past the limit should be elided, not printed:\n%s", out)
	}
	if strings.Contains(out, "low.go") {
		t.Fatalf("LOW block must not be rendered:\n%s", out)
	}
	if !strings.Contains(out, "... 1 more ...") {
		t.Fatalf("expected elision tail '... 1 more ...':\n%s", out)
	}
}

// In CI mode a CRITICAL block is prefixed with a GitHub Actions error annotation.
func TestPrintBlocksCIAnnotation(t *testing.T) {
	merged := []MergedBlock{
		{File: "a.go", StartLine: 7, StartCol: 1, EndLine: 9, EndCol: 1,
			NumLines: 2, EffectiveLines: 2, Level: "CRITICAL", FixAction: "Required"},
	}

	var buf bytes.Buffer
	printBlocks(&buf, merged, 20, 10, false, true)
	out := buf.String()

	if !strings.Contains(out, "::error file=a.go,line=7::") {
		t.Fatalf("missing CI error annotation for critical block:\n%s", out)
	}
}

// The uncovered-blocks header renders the column titles and separators; the
// description block is only emitted when requested.
func TestPrintUncoveredHeader(t *testing.T) {
	var buf bytes.Buffer
	printUncoveredHeader(&buf, 20, false)
	out := buf.String()

	if !strings.Contains(out, "LOCATION") || !strings.Contains(out, "FIX ACTION") {
		t.Fatalf("missing column titles in header:\n%s", out)
	}
	if strings.Contains(out, "Range semantics") {
		t.Fatalf("description block emitted when not requested:\n%s", out)
	}

	var withDesc bytes.Buffer
	printUncoveredHeader(&withDesc, 20, true)
	if !strings.Contains(withDesc.String(), "Range semantics") {
		t.Fatalf("description block missing when requested:\n%s", withDesc.String())
	}
}
