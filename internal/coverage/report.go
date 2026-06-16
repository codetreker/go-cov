package coverage

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// ANSI color codes
const (
	ColorRed    = "\033[31m"
	ColorYellow = "\033[33m"
	ColorGreen  = "\033[32m"
	ColorBlue   = "\033[34m"
	ColorReset  = "\033[0m"
)

// detectColorEnabled reports whether ANSI color escapes should be emitted.
// Color is disabled when the NO_COLOR environment variable is present and
// non-empty (per the https://no-color.org convention) or when stdout is not a
// character device (e.g. redirected to a file or piped), so plain text is
// written to non-terminals. TTY detection uses only the standard library.
func detectColorEnabled() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, err := os.Stdout.Stat()
	isTTY := err == nil && fi.Mode()&os.ModeCharDevice != 0
	return isTTY
}

// colorize wraps s in the given ANSI color code when color is enabled,
// returning s unchanged otherwise. Centralizing this keeps raw escape literals
// out of the rest of the package.
func colorize(s, color string, enabled bool) string {
	if !enabled {
		return s
	}
	return color + s + ColorReset
}

// printPackageSummary prints the package coverage summary table
// Returns true if any package is below threshold (CRITICAL)
func printPackageSummary(out io.Writer, cfg Config, results []PackageResult, topLevelCounts, subTestCounts map[string]int) bool {
	hasCritical := false

	fmt.Fprintln(out, "Package coverage summary:")
	if cfg.ShowTestCounts {
		fmt.Fprintf(out, "%-3s %-40s %-10s %-7s %s\n", "OK", "PACKAGE", "DURATION", "TESTS", "COVERAGE")
	} else {
		fmt.Fprintf(out, "%-3s %-40s %-10s %s\n", "OK", "PACKAGE", "DURATION", "COVERAGE")
	}
	fmt.Fprintln(out, strings.Repeat("-", 80))

	// Sort by coverage descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].Coverage > results[j].Coverage
	})

	for _, r := range results {
		if r.Status == "ok" {
			duration := r.Duration
			if r.Cached {
				duration = "(cached)"
			}

			total := topLevelCounts[r.Name] + subTestCounts[r.Name]
			coverageStr := r.CoverageStr
			isCritical := r.Coverage < cfg.ThresholdPackage

			if isCritical {
				hasCritical = true
				if cfg.CIMode {
					fmt.Fprintf(out, "::error::%-3s %-40s %-10s %s (CRITICAL: < %.0f%%)\n",
						r.Status, r.Name, duration, coverageStr, cfg.ThresholdPackage)
				} else {
					coverageStr = fmt.Sprintf("%s (CRITICAL: < %.0f%%)", colorize(r.CoverageStr, ColorRed, cfg.ColorEnabled), cfg.ThresholdPackage)
					if cfg.ShowTestCounts {
						fmt.Fprintf(out, "%-3s %-40s %-10s %-7d %s\n", r.Status, r.Name, duration, total, coverageStr)
					} else {
						fmt.Fprintf(out, "%-3s %-40s %-10s %s\n", r.Status, r.Name, duration, coverageStr)
					}
				}
			} else {
				if cfg.ShowTestCounts {
					fmt.Fprintf(out, "%-3s %-40s %-10s %-7d %s\n", r.Status, r.Name, duration, total, coverageStr)
				} else {
					fmt.Fprintf(out, "%-3s %-40s %-10s %s\n", r.Status, r.Name, duration, coverageStr)
				}
			}
		}
	}

	// Print failed packages
	for _, r := range results {
		if r.Status == "FAIL" {
			if cfg.CIMode {
				fmt.Fprintf(out, "::error::%-3s %-40s %s\n", r.Status, r.Name, "[FAILED]")
			} else {
				row := fmt.Sprintf("%-3s %-40s %s", r.Status, r.Name, "[FAILED]")
				fmt.Fprintln(out, colorize(row, ColorRed, cfg.ColorEnabled))
			}
			hasCritical = true
		}
	}

	// Print skipped packages
	for _, r := range results {
		if r.Status == "?" {
			fmt.Fprintf(out, "%-3s %-40s %s\n", r.Status, r.Name, "[no test files]")
		}
	}

	return hasCritical
}

// printFunctionCoverage prints function coverage details
// Returns (hasCritical, totalWidth) where totalWidth is for alignment
func printFunctionCoverage(out io.Writer, cfg Config, funcs []FuncCoverage) (bool, int) {
	hasCritical := false

	// Count functions above threshold and collect below threshold
	aboveThreshold := 0
	var belowThreshold []FuncCoverage

	for _, f := range funcs {
		if f.Coverage >= cfg.ThresholdPrint {
			aboveThreshold++
		} else {
			belowThreshold = append(belowThreshold, f)
		}
	}

	// Calculate max widths for alignment
	maxLocWidth := 20
	maxFuncWidth := 10
	for _, f := range belowThreshold {
		if len(f.Location) > maxLocWidth {
			maxLocWidth = len(f.Location)
		}
		if len(f.Function) > maxFuncWidth {
			maxFuncWidth = len(f.Function)
		}
	}

	totalWidth := maxLocWidth + maxFuncWidth + 15

	fmt.Fprintf(out, "\nFunction coverage details (excluding >= %.0f%%):\n", cfg.ThresholdPrint)
	fmt.Fprintf(out, "%-*s %-*s %s\n", maxLocWidth, "LOCATION", maxFuncWidth, "FUNCTION", "COVERAGE")
	fmt.Fprintln(out, strings.Repeat("-", totalWidth))

	fmt.Fprintf(out, "... %d more...\n", aboveThreshold)

	// Sort by coverage descending
	sort.Slice(belowThreshold, func(i, j int) bool {
		return belowThreshold[i].Coverage > belowThreshold[j].Coverage
	})

	for _, f := range belowThreshold {
		isCritical := f.Coverage < cfg.ThresholdFunc
		if isCritical {
			hasCritical = true
		}

		covStr := fmt.Sprintf("%.1f%%", f.Coverage)
		if isCritical {
			if cfg.CIMode {
				// Parse file and line from location
				parts := strings.Split(f.Location, ":")
				file := parts[0]
				line := "1"
				if len(parts) > 1 {
					line = parts[1]
				}
				fmt.Fprintf(out, "::error file=%s,line=%s::%-*s %-*s %s (CRITICAL < %.0f%%)\n",
					file, line, maxLocWidth, f.Location, maxFuncWidth, f.Function, covStr, cfg.ThresholdFunc)
			} else {
				covStr = fmt.Sprintf("%s (CRITICAL: < %.0f%%)", colorize(fmt.Sprintf("%.1f%%", f.Coverage), ColorRed, cfg.ColorEnabled), cfg.ThresholdFunc)
				fmt.Fprintf(out, "%-*s %-*s %s\n", maxLocWidth, f.Location, maxFuncWidth, f.Function, covStr)
			}
		} else {
			fmt.Fprintf(out, "%-*s %-*s %s\n", maxLocWidth, f.Location, maxFuncWidth, f.Function, covStr)
		}
	}

	fmt.Fprintln(out, strings.Repeat("-", totalWidth))
	return hasCritical, totalWidth
}

// printTotal prints total coverage and checks threshold.
// known reports whether totalCov was actually determined from coverage data;
// when false the total is unknown ("no data"), printed as "n/a", and never
// treated as CRITICAL. Returns true only when a known total is below threshold.
func printTotal(out io.Writer, cfg Config, totalCov float64, known bool, width int) bool {
	labelWidth := width - 10 // Leave space for coverage value
	if labelWidth < 10 {
		labelWidth = 10
	}

	// An undeterminable total is "no data", not 0% coverage: report it as
	// unknown and never let it trip the threshold or fail CI.
	if !known {
		fmt.Fprintf(out, "%-*s %s\n", labelWidth, "TOTAL", "n/a")
		fmt.Fprintln(out, strings.Repeat("-", width))
		return false
	}

	isCritical := totalCov < cfg.ThresholdTotal

	totalStr := fmt.Sprintf("%.1f%%", totalCov)

	if isCritical {
		if cfg.CIMode {
			fmt.Fprintf(out, "::error::%-*s %s (CRITICAL: < %.0f%%)\n", labelWidth, "TOTAL", totalStr, cfg.ThresholdTotal)
		} else {
			totalStr = fmt.Sprintf("%s (CRITICAL: < %.0f%%)", colorize(fmt.Sprintf("%.1f%%", totalCov), ColorRed, cfg.ColorEnabled), cfg.ThresholdTotal)
			fmt.Fprintf(out, "%-*s %s\n", labelWidth, "TOTAL", totalStr)
		}
	} else {
		fmt.Fprintf(out, "%-*s %s\n", labelWidth, "TOTAL", totalStr)
	}
	fmt.Fprintln(out, strings.Repeat("-", width))

	return isCritical
}

// printStatistics prints coverage statistics
func printStatistics(out io.Writer, cfg Config, funcs []FuncCoverage, topLevelCounts, subTestCounts map[string]int) {
	count100 := 0
	count95_100 := 0
	count85_95 := 0
	countLt85 := 0

	for _, f := range funcs {
		switch {
		case f.Coverage == 100.0:
			count100++
		case f.Coverage >= 95.0:
			count95_100++
		case f.Coverage >= 85.0:
			count85_95++
		default:
			countLt85++
		}
	}

	fmt.Fprintln(out, "Statistics:")
	fmt.Fprintf(out, "Functions with 100%% coverage: %d\n", count100)
	fmt.Fprintf(out, "Functions with 95%%-100%% coverage: %d\n", count95_100)
	fmt.Fprintf(out, "Functions with 85%%-95%% coverage: %d\n", count85_95)
	fmt.Fprintf(out, "Functions with <85%% coverage: %d\n", countLt85)

	// Count total tests
	if cfg.ShowTestCounts {
		totalTopLevel := 0
		totalSubTests := 0
		for _, count := range topLevelCounts {
			totalTopLevel += count
		}
		for _, count := range subTestCounts {
			totalSubTests += count
		}
		fmt.Fprintf(out, "Total tests: %d (including subtests, %d top-level)\n", totalTopLevel+totalSubTests, totalTopLevel)
	}
}
