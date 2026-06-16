package coverage

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// TestEvent represents a single JSON event from go test -json
type TestEvent struct {
	Time    string  `json:"Time"`
	Action  string  `json:"Action"`
	Package string  `json:"Package"`
	Test    string  `json:"Test"`
	Output  string  `json:"Output"`
	Elapsed float64 `json:"Elapsed"`
	// ImportPath is set on build-output/build-fail events, which carry no Package.
	// It looks like "example.com/mod/pkg [example.com/mod/pkg.test]".
	ImportPath string `json:"ImportPath"`
}

// PackageResult holds test results for a package
type PackageResult struct {
	Name        string
	Status      string // "ok", "FAIL", "?"
	Duration    string
	Coverage    float64
	CoverageStr string
	TestCount   int
	Cached      bool
}

// FuncCoverage represents function-level coverage
type FuncCoverage struct {
	Location string
	Function string
	Coverage float64
}

// parseTestOutput parses JSON output from go test
func parseTestOutput(out io.Writer, cfg Config, r io.Reader) ([]PackageResult, map[string]int, map[string]int) {
	results := make(map[string]*PackageResult)
	topLevelCounts := make(map[string]int)
	subTestCounts := make(map[string]int)
	// Read line-by-line rather than streaming through a json.Decoder: a Decoder
	// does not advance past a malformed token, so a single non-JSON line would
	// make every subsequent Decode return the same error and spin the loop. We
	// use bufio.Reader.ReadString instead of bufio.Scanner because Scanner caps
	// lines at 64KB and `go test -json` output lines can exceed that.
	br := bufio.NewReader(r)

	// Track which packages we've printed and their start times
	printedPackages := make(map[string]bool)
	packageStartTimes := make(map[string]time.Time)

	// Buffer output per test, only print on failure
	// Key: "pkg/test" or just "pkg" for package-level output
	testOutputs := make(map[string][]string)

	// Buffer compiler/vet errors per package. These arrive as build-output events
	// that carry an ImportPath (not a Package) and are otherwise lost when the
	// package later reports a build failure.
	buildOutputs := make(map[string][]string)

	for {
		line, readErr := br.ReadString('\n')

		if trimmed := strings.TrimSpace(line); trimmed != "" && strings.HasPrefix(trimmed, "{") {
			var event TestEvent
			if err := json.Unmarshal([]byte(trimmed), &event); err == nil {
				processTestEvent(out, cfg, event, results, topLevelCounts, subTestCounts,
					printedPackages, packageStartTimes, testOutputs, buildOutputs)
			}
		}

		if readErr != nil {
			// io.EOF (and any other read error) terminates parsing. ReadString
			// may return a final partial line alongside the error, which the
			// block above has already handled.
			break
		}
	}

	// Convert map to slice
	var resultSlice []PackageResult
	for _, r := range results {
		resultSlice = append(resultSlice, *r)
	}

	return resultSlice, topLevelCounts, subTestCounts
}

// processTestEvent applies a single decoded go-test JSON event to the running
// parse state: buffering build/test output, printing package start/finish and
// failure detail, and counting top-level vs subtests.
func processTestEvent(
	out io.Writer,
	cfg Config,
	event TestEvent,
	results map[string]*PackageResult,
	topLevelCounts, subTestCounts map[string]int,
	printedPackages map[string]bool,
	packageStartTimes map[string]time.Time,
	testOutputs, buildOutputs map[string][]string,
) {

	// Build failures stream their detail as build-output events keyed by
	// ImportPath, with an empty Package. Buffer them under the package name so
	// they can be printed when the package's fail event arrives.
	if event.Action == "build-output" {
		bpkg := buildFailurePackage(cfg, event.ImportPath)
		buildOutputs[bpkg] = append(buildOutputs[bpkg], event.Output)
		return
	}

	pkg := cfg.stripModulePrefix(event.Package)

	// Build key for output buffering
	outputKey := pkg
	if event.Test != "" {
		outputKey = pkg + "/" + event.Test
	}

	// Count tests separately: top-level vs subtests
	if event.Action == "run" && event.Test != "" {
		// Print package start only once (when first test runs)
		if !printedPackages[pkg] {
			now := time.Now()
			packageStartTimes[pkg] = now
			fmt.Fprintf(out, "[%s] Testing %s...\n", now.Format("15:04:05"), pkg)
			printedPackages[pkg] = true
		}

		if strings.Contains(event.Test, "/") {
			subTestCounts[pkg]++
		} else {
			topLevelCounts[pkg]++
		}
	}

	// Print package completion time
	if event.Action == "pass" && event.Test == "" {
		if startTime, ok := packageStartTimes[pkg]; ok {
			elapsed := time.Since(startTime)
			fmt.Fprintf(out, "[%s] ✓ %s completed (%.2fs)\n", time.Now().Format("15:04:05"), pkg, elapsed.Seconds())
		}
	}

	// Buffer output for each test
	if event.Action == "output" {
		// Parse coverage/result lines
		if strings.HasPrefix(event.Output, "ok") {
			parsePackageResult(cfg, event.Output, results)
		} else if strings.HasPrefix(event.Output, "FAIL") {
			parsePackageResult(cfg, event.Output, results)
		} else if strings.HasPrefix(event.Output, "?") {
			parsePackageResult(cfg, event.Output, results)
		} else {
			// Buffer output for potential failure
			testOutputs[outputKey] = append(testOutputs[outputKey], event.Output)
		}
	}

	// On test failure, print buffered output
	if event.Action == "fail" && event.Test != "" {
		if outputs, ok := testOutputs[outputKey]; ok {
			fmt.Fprintf(out, "\n=== FAIL: %s/%s ===\n", pkg, event.Test)
			for _, o := range outputs {
				fmt.Fprint(out, o)
			}
		}
		delete(testOutputs, outputKey)
	}

	// On package-level failure, surface the reason: a build error if the
	// package failed to compile, otherwise any buffered package-scope output
	// (e.g. a panic in a goroutine or output before a crash) that was never
	// attributed to an individual test.
	if event.Action == "fail" && event.Test == "" {
		if bo, ok := buildOutputs[pkg]; ok {
			printBuildFailure(out, cfg, pkg, bo)
			delete(buildOutputs, pkg)
		} else if outputs, ok := testOutputs[pkg]; ok && len(outputs) > 0 {
			printPackageFailure(out, cfg, pkg, outputs)
		}
		delete(testOutputs, pkg)
	}

	// Clean up on pass
	if event.Action == "pass" && event.Test != "" {
		delete(testOutputs, outputKey)
	}
}

// buildFailurePackage extracts the display package name from a build event's
// ImportPath, e.g. "example.com/mod/pkg [example.com/mod/pkg.test]" -> "pkg"
// after the module prefix is stripped.
func buildFailurePackage(cfg Config, importPath string) string {
	pkg := importPath
	if idx := strings.Index(pkg, " ["); idx != -1 {
		pkg = pkg[:idx]
	}
	return cfg.stripModulePrefix(strings.TrimSpace(pkg))
}

// printBuildFailure reports a package that failed to compile. In CI mode each
// compiler error becomes a GitHub Actions annotation pinned to its file/line;
// locally it prints a human-readable block.
func printBuildFailure(out io.Writer, cfg Config, pkg string, lines []string) {
	if cfg.CIMode {
		for _, o := range lines {
			if file, lineNo, msg, ok := parseCompilerError(o); ok {
				fmt.Fprintf(out, "::error file=%s,line=%s::%s\n", file, lineNo, msg)
			} else if strings.TrimSpace(o) != "" {
				fmt.Fprint(out, o)
			}
		}
		return
	}
	fmt.Fprintf(out, "\n=== BUILD FAILED: %s ===\n", pkg)
	for _, o := range lines {
		fmt.Fprint(out, o)
	}
}

// printPackageFailure reports package-scope failure output (a panic or other
// output not attributed to a single test). In CI mode it is prefixed with a
// GitHub Actions error annotation.
func printPackageFailure(out io.Writer, cfg Config, pkg string, lines []string) {
	if cfg.CIMode {
		fmt.Fprintf(out, "::error::package %s failed\n", pkg)
	} else {
		fmt.Fprintf(out, "\n=== FAIL: %s ===\n", pkg)
	}
	for _, o := range lines {
		fmt.Fprint(out, o)
	}
}

// parseCompilerError parses a Go compiler diagnostic of the form
// "file:line[:col]: message". The optional column is dropped.
func parseCompilerError(line string) (file, lineNo, msg string, ok bool) {
	s := strings.TrimRight(line, "\r\n")

	i1 := strings.IndexByte(s, ':')
	if i1 <= 0 {
		return "", "", "", false
	}
	rest := s[i1+1:]

	i2 := strings.IndexByte(rest, ':')
	if i2 < 0 {
		return "", "", "", false
	}
	lineNo = rest[:i2]
	if !isAllDigits(lineNo) {
		return "", "", "", false
	}

	file = s[:i1]
	after := rest[i2+1:] // either "col: message" or " message"
	if j := strings.IndexByte(after, ':'); j >= 0 && isAllDigits(after[:j]) {
		msg = strings.TrimSpace(after[j+1:])
	} else {
		msg = strings.TrimSpace(after)
	}
	return file, lineNo, msg, true
}

// isAllDigits reports whether s is non-empty and contains only ASCII digits.
func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// parsePackageResult parses a single package result line
func parsePackageResult(cfg Config, line string, results map[string]*PackageResult) {
	line = strings.TrimSpace(line)
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return
	}

	status := fields[0]
	pkg := cfg.stripModulePrefix(fields[1])
	for _, ex := range cfg.SkipResultPackages {
		if ex != "" && strings.Contains(pkg, ex) {
			return
		}
	}

	result := &PackageResult{
		Name:   pkg,
		Status: status,
	}

	if status == "ok" && len(fields) >= 3 {
		result.Duration = fields[2]
		result.Cached = strings.Contains(line, "(cached)")

		// Parse coverage percentage
		for _, f := range fields {
			if strings.HasSuffix(f, "%") {
				f = strings.TrimSuffix(f, "%")
				if cov, err := strconv.ParseFloat(f, 64); err == nil {
					result.Coverage = cov
					result.CoverageStr = fmt.Sprintf("%.1f%%", cov)
				}
			}
		}
	}

	results[pkg] = result
}

func parseFunctionCoverageOutput(output string, c Config) []FuncCoverage {
	var funcs []FuncCoverage
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		line = c.stripModulePrefix(line)

		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}

		if fields[0] == "total:" {
			continue
		}

		// Skip excluded files/functions
		excluded := false
		for _, ex := range c.ExcludeFiles {
			if ex != "" && strings.Contains(fields[0], ex) {
				excluded = true
				break
			}
		}
		// Skip functions whose name ends in a configured suffix (e.g. "ForTest"
		// helpers that live in non-test files). See Config.ExcludeFuncSuffixes.
		for _, suffix := range c.ExcludeFuncSuffixes {
			if suffix != "" && strings.HasSuffix(fields[1], suffix) {
				excluded = true
				break
			}
		}
		// Skip excluded functions by name substring.
		for _, ef := range c.ExcludeFuncs {
			if ef != "" && strings.Contains(fields[1], ef) {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}

		covStr := strings.TrimSuffix(fields[len(fields)-1], "%")
		cov, _ := strconv.ParseFloat(covStr, 64)

		funcs = append(funcs, FuncCoverage{
			Location: fields[0],
			Function: fields[1],
			Coverage: cov,
		})
	}

	return funcs
}

// parseTotalCoverageOutput extracts the overall coverage percentage from the
// "total:" line of `go tool cover -func` output. The boolean return reports
// whether a total was found: it is false when the output is empty or has no
// parseable "total:" line, which means "no data" rather than a genuine 0.0%.
func parseTotalCoverageOutput(output string) (float64, bool) {
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "total:") {
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				covStr := strings.TrimSuffix(fields[len(fields)-1], "%")
				cov, err := strconv.ParseFloat(covStr, 64)
				if err != nil {
					return 0, false
				}
				return cov, true
			}
		}
	}
	return 0, false
}
