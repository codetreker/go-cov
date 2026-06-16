package coverage

import (
	"strings"
	"testing"
)

// A below-threshold (CRITICAL) package in local mode must respect ShowTestCounts:
// when it is false the test-count column is omitted so the row aligns with the
// header, which also omits the TESTS column.
func TestPackageSummaryCriticalRowOmitsTestCountWhenDisabled(t *testing.T) {
	cfg = Config{ThresholdPackage: 85.0, ShowTestCounts: false}

	results := []PackageResult{
		{Name: "pkg", Status: "ok", Duration: "0.10s", Coverage: 50.0, CoverageStr: "50.0%"},
	}
	// A distinctive count that cannot collide with the row's other numbers
	// (duration, coverage, threshold) or ANSI escape codes.
	topLevel := map[string]int{"pkg": 7}
	subTests := map[string]int{"pkg": 0}

	out := captureStdout(t, func() {
		printPackageSummary(results, topLevel, subTests)
	})

	// The header omits TESTS, so the CRITICAL row must not carry a test count.
	if strings.Contains(out, "TESTS") {
		t.Fatalf("header should omit TESTS column when ShowTestCounts is false:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "CRITICAL") && strings.Contains(stripANSI(line), "7") {
			t.Fatalf("CRITICAL row leaked the test-count column when ShowTestCounts is false:\n%s", line)
		}
	}
}

// stripANSI removes ANSI color escape sequences so digit assertions are not
// fooled by codes like "\033[31m".
func stripANSI(s string) string {
	s = strings.ReplaceAll(s, ColorRed, "")
	s = strings.ReplaceAll(s, ColorYellow, "")
	s = strings.ReplaceAll(s, ColorBlue, "")
	s = strings.ReplaceAll(s, ColorReset, "")
	return s
}

// With ShowTestCounts enabled, the CRITICAL row must still include the
// per-package test count (unchanged behavior).
func TestPackageSummaryCriticalRowKeepsTestCountWhenEnabled(t *testing.T) {
	cfg = Config{ThresholdPackage: 85.0, ShowTestCounts: true}

	results := []PackageResult{
		{Name: "pkg", Status: "ok", Duration: "0.10s", Coverage: 42.0, CoverageStr: "42.0%"},
	}
	// total = topLevel + subTests = 9, a digit that appears nowhere else in the
	// row (duration, coverage, threshold) or in ANSI escape codes.
	topLevel := map[string]int{"pkg": 6}
	subTests := map[string]int{"pkg": 3}

	out := captureStdout(t, func() {
		printPackageSummary(results, topLevel, subTests)
	})

	var criticalLine string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "CRITICAL") {
			criticalLine = line
			break
		}
	}
	if criticalLine == "" {
		t.Fatalf("expected a CRITICAL row in output:\n%s", out)
	}
	// The count column must be present.
	if !strings.Contains(stripANSI(criticalLine), "9") {
		t.Fatalf("CRITICAL row dropped the test-count column when ShowTestCounts is true:\n%s", criticalLine)
	}
}
