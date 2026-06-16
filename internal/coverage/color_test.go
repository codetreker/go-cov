package coverage

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// containsANSI reports whether s contains an ANSI escape introducer (\033[),
// which is the prefix of every color code this tool emits.
func containsANSI(s string) bool {
	return strings.Contains(s, "\033[")
}

// detectColorEnabled is the actual NO_COLOR/TTY decision. NO_COLOR disables color
// unconditionally; otherwise color is enabled only for an *os.File that is a
// character device (a terminal). Non-file and non-terminal destinations are plain.
func TestDetectColorEnabled(t *testing.T) {
	// NO_COLOR set => disabled even for a real terminal handle.
	t.Setenv("NO_COLOR", "1")
	if detectColorEnabled(os.Stdout) {
		t.Fatal("color must be disabled when NO_COLOR is set")
	}

	// Empty NO_COLOR is treated as unset (per the convention). A non-*os.File
	// writer is never a terminal.
	t.Setenv("NO_COLOR", "")
	if detectColorEnabled(&bytes.Buffer{}) {
		t.Fatal("color must be disabled for a non-*os.File writer")
	}

	// A regular file is an *os.File but not a character device.
	f, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if detectColorEnabled(f) {
		t.Fatal("color must be disabled when writing to a regular file")
	}
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

	var buf bytes.Buffer
	printPackageSummary(&buf, cfg, results, map[string]int{}, map[string]int{})
	out := buf.String()

	if containsANSI(out) {
		t.Fatalf("output contained ANSI escapes when color disabled:\n%q", out)
	}
	// The plain text content must still be present.
	if !strings.Contains(out, "CRITICAL") || !strings.Contains(out, "[FAILED]") {
		t.Fatalf("expected CRITICAL and [FAILED] markers in output:\n%s", out)
	}
}

// With color enabled, the below-threshold package and the FAIL row must both be
// colorized (the FAIL row is a distinct colorized branch).
func TestPackageSummaryEmitsANSIWhenColorEnabled(t *testing.T) {
	cfg := Config{ThresholdPackage: 85.0, ShowTestCounts: true, ColorEnabled: true}

	results := []PackageResult{
		{Name: "low", Status: "ok", Duration: "0.10s", Coverage: 50.0, CoverageStr: "50.0%"},
		{Name: "broken", Status: "FAIL"},
	}

	var buf bytes.Buffer
	printPackageSummary(&buf, cfg, results, map[string]int{}, map[string]int{})
	out := buf.String()

	if !strings.Contains(out, ColorRed) {
		t.Fatalf("expected ANSI red escape when color enabled:\n%q", out)
	}

	// The FAIL row specifically must be wrapped in color.
	var failLine string
	for _, ln := range strings.Split(out, "\n") {
		if strings.Contains(ln, "[FAILED]") {
			failLine = ln
			break
		}
	}
	if failLine == "" {
		t.Fatalf("no [FAILED] row in output:\n%s", out)
	}
	if !containsANSI(failLine) {
		t.Fatalf("FAIL row not colorized when color enabled: %q", failLine)
	}
}

// A CRITICAL uncovered block must render without ANSI escapes when color is
// disabled, while keeping the plain level and fix-action text.
func TestMergedBlockPrintNoANSIWhenColorDisabled(t *testing.T) {
	block := MergedBlock{
		File: "foo/bar.go", StartLine: 10, StartCol: 5, EndLine: 13, EndCol: 1,
		NumLines: 3, EffectiveLines: 3, Level: "CRITICAL", FixAction: "Required",
	}

	var buf bytes.Buffer
	block.Print(&buf, 20, false, false)
	out := buf.String()

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

	var buf bytes.Buffer
	block.Print(&buf, 20, true, false)
	out := buf.String()

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
