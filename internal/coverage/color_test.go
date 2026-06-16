package coverage

import (
	"strings"
	"testing"
)

// containsANSI reports whether s contains an ANSI escape introducer (\033[),
// which is the prefix of every color code this tool emits.
func containsANSI(s string) bool {
	return strings.Contains(s, "\033[")
}

// With color disabled, rendered output must be plain text with no ANSI escapes,
// so piping or redirecting to a file produces clean output. A below-threshold
// package and a failed package both exercise the colorized local-mode branches.
func TestPackageSummaryNoANSIWhenColorDisabled(t *testing.T) {
	cfg := Config{ThresholdPackage: 85.0, ShowTestCounts: true, ColorEnabled: false}

	results := []PackageResult{
		{Name: "low", Status: "ok", Duration: "0.10s", Coverage: 50.0, CoverageStr: "50.0%"},
		{Name: "broken", Status: "FAIL"},
	}

	out := captureStdout(t, func() {
		printPackageSummary(cfg, results, map[string]int{}, map[string]int{})
	})

	if containsANSI(out) {
		t.Fatalf("output contained ANSI escapes when color disabled:\n%q", out)
	}
	// The plain text content must still be present.
	if !strings.Contains(out, "CRITICAL") || !strings.Contains(out, "[FAILED]") {
		t.Fatalf("expected CRITICAL and [FAILED] markers in output:\n%s", out)
	}
}

// With color enabled, the same below-threshold package must still be colorized.
func TestPackageSummaryEmitsANSIWhenColorEnabled(t *testing.T) {
	cfg := Config{ThresholdPackage: 85.0, ShowTestCounts: true, ColorEnabled: true}

	results := []PackageResult{
		{Name: "low", Status: "ok", Duration: "0.10s", Coverage: 50.0, CoverageStr: "50.0%"},
	}

	out := captureStdout(t, func() {
		printPackageSummary(cfg, results, map[string]int{}, map[string]int{})
	})

	if !strings.Contains(out, ColorRed) {
		t.Fatalf("expected ANSI red escape when color enabled:\n%q", out)
	}
}

// A CRITICAL uncovered block must render without ANSI escapes when color is
// disabled, while keeping the plain level and fix-action text.
func TestMergedBlockPrintNoANSIWhenColorDisabled(t *testing.T) {
	block := MergedBlock{
		File: "foo/bar.go", StartLine: 10, StartCol: 5, EndLine: 13, EndCol: 1,
		NumLines: 3, EffectiveLines: 3, Level: "CRITICAL", FixAction: "Required",
	}

	out := captureStdout(t, func() {
		block.Print(20, false, false)
	})

	if containsANSI(out) {
		t.Fatalf("block output contained ANSI escapes when color disabled:\n%q", out)
	}
	if !strings.Contains(out, "CRITICAL") || !strings.Contains(out, "Required") {
		t.Fatalf("expected plain CRITICAL/Required text in output:\n%s", out)
	}
}

// The same block must be colorized when color is enabled.
func TestMergedBlockPrintEmitsANSIWhenColorEnabled(t *testing.T) {
	block := MergedBlock{
		File: "foo/bar.go", StartLine: 10, StartCol: 5, EndLine: 13, EndCol: 1,
		NumLines: 3, EffectiveLines: 3, Level: "CRITICAL", FixAction: "Required",
	}

	out := captureStdout(t, func() {
		block.Print(20, true, false)
	})

	if !strings.Contains(out, ColorRed) {
		t.Fatalf("expected ANSI red escape when color enabled:\n%q", out)
	}
}

// FixAction and Level must store plain text, not embedded escape sequences, so
// color can be applied (or skipped) at print time.
func TestFixActionIsPlainText(t *testing.T) {
	if got := getFixAction("CRITICAL"); got != "Required" {
		t.Fatalf("getFixAction(CRITICAL) = %q, want plain %q", got, "Required")
	}
}
