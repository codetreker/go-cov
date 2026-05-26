// Package coverage provides a Go-based coverage analysis tool.
// It runs tests, collects coverage data, and generates comprehensive reports.
package coverage

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Config holds the configuration for coverage analysis
type Config struct {
	// Thresholds
	ThresholdFunc    float64
	ThresholdPackage float64
	ThresholdPrint   float64
	ThresholdTotal   float64

	// Behavior
	CIMode               bool     // Enable CI mode (GitHub Actions error format, fail on CRITICAL)
	RaceDetection        bool     // Enable -race flag
	ModulePrefix         string   // Go module import prefix stripped from display paths
	ProjectName          string   // Project name used for default local HTML output
	ExcludePackages      []string // Packages to exclude by substring
	SkipResultPackages   []string // Package display names to skip from summaries by substring
	ExcludeFiles         []string // File path substrings to exclude from function/block reports
	ExcludeFuncs         []string // Function names to exclude from coverage threshold
	CoverProfile         string   // Coverage profile path
	HTMLPath             string   // HTML coverage report output path
	TestTimeout          string   // go test timeout value
	BuildTags            string   // Build tags passed to go test
	GenerateHTML         bool     // Generate local HTML coverage report when not in CI mode
	FailOnCriticalBlocks bool     // In CI mode, fail on AST critical uncovered blocks
	UncoveredLimit       int      // Max uncovered blocks to show
	ShowTestCounts       bool     // Show TESTS column in package summary
}

// ANSI color codes
const (
	ColorRed    = "\033[31m"
	ColorYellow = "\033[33m"
	ColorBlue   = "\033[34m"
	ColorReset  = "\033[0m"
)

// TestEvent represents a single JSON event from go test -json
type TestEvent struct {
	Time    string  `json:"Time"`
	Action  string  `json:"Action"`
	Package string  `json:"Package"`
	Test    string  `json:"Test"`
	Output  string  `json:"Output"`
	Elapsed float64 `json:"Elapsed"`
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

// Global config, retained internally to keep the extracted implementation small.
var cfg Config

// Run executes the coverage workflow and returns a process-style exit code.
func Run(c Config) int {
	cfg = normalizeConfig(c)

	fmt.Println()
	startTime := time.Now()
	fmt.Printf("[%s] Starting test run...\n\n", startTime.Format("15:04:05"))

	// Run tests and collect results
	results, topLevelCounts, subTestCounts, exitCode := runTests()
	if exitCode != 0 && len(results) == 0 {
		return exitCode
	}

	testDuration := time.Since(startTime)
	fmt.Printf("\n[%s] Test run completed in %.2fs\n", time.Now().Format("15:04:05"), testDuration.Seconds())
	fmt.Println(strings.Repeat("=", 80))
	fmt.Println()

	// Print package coverage summary
	hasCriticalPackage := printPackageSummary(results, topLevelCounts, subTestCounts)

	if exitCode != 0 {
		return exitCode
	}

	// Get function coverage data
	funcData := getFunctionCoverage()

	// Print function coverage details
	hasCriticalFunc, funcWidth := printFunctionCoverage(funcData)

	// Print total and check threshold
	hasCriticalTotal := printTotal(funcWidth)

	// Print statistics
	printStatistics(funcData, topLevelCounts, subTestCounts)

	// Generate HTML report (only in local mode)
	if !cfg.CIMode && cfg.GenerateHTML {
		generateHTMLReport()
	}

	// Analyze uncovered blocks
	fmt.Println()
	fmt.Println(strings.Repeat("-", 90))
	hasCriticalBlocks := analyzeUncoveredBlocks()

	// In CI mode, exit with error if any CRITICAL issues
	if cfg.CIMode && (hasCriticalPackage || hasCriticalFunc || hasCriticalTotal || (cfg.FailOnCriticalBlocks && hasCriticalBlocks)) {
		return 1
	}

	return 0
}

// DefaultConfig returns conservative defaults suitable for most Go projects.
func DefaultConfig() Config {
	modulePath := detectModulePath()
	projectName := projectNameFromModule(modulePath)

	c := Config{
		ThresholdFunc:        80.0,
		ThresholdPackage:     85.0,
		ThresholdPrint:       85.0,
		ThresholdTotal:       90.0,
		CIMode:               os.Getenv("CI") == "true",
		RaceDetection:        false,
		ModulePrefix:         normalizeModulePrefix(modulePath),
		ProjectName:          projectName,
		ExcludePackages:      nil,
		SkipResultPackages:   nil,
		ExcludeFiles:         nil,
		ExcludeFuncs:         nil,
		CoverProfile:         "/tmp/coverage.out",
		HTMLPath:             defaultHTMLPath(projectName),
		TestTimeout:          "15m",
		BuildTags:            "",
		GenerateHTML:         true,
		FailOnCriticalBlocks: true,
		UncoveredLimit:       10,
		ShowTestCounts:       true,
	}

	// CI mode has different defaults
	if c.CIMode {
		c.ThresholdPrint = 90.0
		c.RaceDetection = true
		c.CoverProfile = "coverage.out"
		c.UncoveredLimit = 20
		// ShowTestCounts remains true in CI mode
	}

	return c
}

// ConfigFromEnv parses configuration from environment variables and CLI arguments.
func ConfigFromEnv(args []string) (Config, error) {
	c := DefaultConfig()

	// Override from environment first; flags win below.
	if v := os.Getenv("COVERPROFILE"); v != "" {
		c.CoverProfile = v
	}
	if v := os.Getenv("MODULE_PREFIX"); v != "" {
		c.ModulePrefix = normalizeModulePrefix(v)
	}
	if v := os.Getenv("PROJECT_NAME"); v != "" {
		c.ProjectName = v
		c.HTMLPath = defaultHTMLPath(v)
	}
	if v := os.Getenv("HTML_OUT"); v != "" {
		c.HTMLPath = v
	}
	if v := os.Getenv("THRESHOLD_FUNC"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			c.ThresholdFunc = f
		}
	}
	if v := os.Getenv("THRESHOLD_PACKAGE"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			c.ThresholdPackage = f
		}
	}
	if v := os.Getenv("THRESHOLD_PRINT"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			c.ThresholdPrint = f
		}
	}
	if v := os.Getenv("THRESHOLD_TOTAL"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			c.ThresholdTotal = f
		}
	}
	if v := os.Getenv("UNCOVERED_LIMIT"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			c.UncoveredLimit = i
		}
	}
	if v := os.Getenv("EXCLUDE_PACKAGES"); v != "" {
		c.ExcludePackages = splitCSV(v)
	}
	if v := os.Getenv("SKIP_RESULT_PACKAGES"); v != "" {
		c.SkipResultPackages = splitCSV(v)
	}
	if v := os.Getenv("EXCLUDE_FILES"); v != "" {
		c.ExcludeFiles = splitCSV(v)
	}
	if v := os.Getenv("EXCLUDE_FUNCS"); v != "" {
		c.ExcludeFuncs = splitCSV(v)
	}
	if v := os.Getenv("TEST_TIMEOUT"); v != "" {
		c.TestTimeout = v
	}
	if v := os.Getenv("BUILD_TAGS"); v != "" {
		c.BuildTags = v
	}
	if v := os.Getenv("GENERATE_HTML"); v != "" {
		c.GenerateHTML = v == "true"
	}
	if v := os.Getenv("RACE_DETECTION"); v != "" {
		c.RaceDetection = v == "true"
	}
	if v := os.Getenv("FAIL_ON_CRITICAL_BLOCKS"); v != "" {
		c.FailOnCriticalBlocks = v == "true"
	}

	fs := flag.NewFlagSet("go-cov", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	modulePrefix := fs.String("module-prefix", c.ModulePrefix, "module import prefix to strip from output paths")
	projectName := fs.String("project", c.ProjectName, "project name used for default local HTML output")
	coverProfile := fs.String("coverprofile", c.CoverProfile, "coverage profile path")
	htmlPath := fs.String("html-out", c.HTMLPath, "HTML coverage report output path")
	excludePackages := fs.String("exclude-packages", joinCSV(c.ExcludePackages), "comma-separated package substrings to exclude")
	skipResultPackages := fs.String("skip-result-packages", joinCSV(c.SkipResultPackages), "comma-separated displayed package substrings to omit from summaries")
	excludeFiles := fs.String("exclude-files", joinCSV(c.ExcludeFiles), "comma-separated file substrings to exclude")
	excludeFuncs := fs.String("exclude-funcs", joinCSV(c.ExcludeFuncs), "comma-separated function name substrings to exclude")
	testTimeout := fs.String("timeout", c.TestTimeout, "go test timeout")
	buildTags := fs.String("tags", c.BuildTags, "build tags passed to go test")
	thresholdFunc := fs.Float64("threshold-func", c.ThresholdFunc, "function critical threshold")
	thresholdPackage := fs.Float64("threshold-package", c.ThresholdPackage, "package critical threshold")
	thresholdPrint := fs.Float64("threshold-print", c.ThresholdPrint, "print functions below this threshold")
	thresholdTotal := fs.Float64("threshold-total", c.ThresholdTotal, "total critical threshold")
	uncoveredLimit := fs.Int("uncovered-limit", c.UncoveredLimit, "max uncovered blocks to print")
	ciMode := fs.Bool("ci", c.CIMode, "enable CI/GitHub Actions error output")
	raceDetection := fs.Bool("race", c.RaceDetection, "enable go test -race")
	generateHTML := fs.Bool("generate-html", c.GenerateHTML, "generate local HTML coverage report outside CI")
	failOnCriticalBlocks := fs.Bool("fail-on-critical-blocks", c.FailOnCriticalBlocks, "fail CI on AST critical uncovered blocks")
	showTestCounts := fs.Bool("show-test-counts", c.ShowTestCounts, "show TESTS column in package summary")

	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	projectFlagSet := false
	htmlFlagSet := false
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "project":
			projectFlagSet = true
		case "html-out":
			htmlFlagSet = true
		}
	})

	c.ModulePrefix = normalizeModulePrefix(*modulePrefix)
	c.ProjectName = *projectName
	c.CoverProfile = *coverProfile
	c.HTMLPath = *htmlPath
	if projectFlagSet && !htmlFlagSet {
		c.HTMLPath = defaultHTMLPath(c.ProjectName)
	}
	c.ExcludePackages = splitCSV(*excludePackages)
	c.SkipResultPackages = splitCSV(*skipResultPackages)
	c.ExcludeFiles = splitCSV(*excludeFiles)
	c.ExcludeFuncs = splitCSV(*excludeFuncs)
	c.TestTimeout = *testTimeout
	c.BuildTags = *buildTags
	c.ThresholdFunc = *thresholdFunc
	c.ThresholdPackage = *thresholdPackage
	c.ThresholdPrint = *thresholdPrint
	c.ThresholdTotal = *thresholdTotal
	c.UncoveredLimit = *uncoveredLimit
	c.CIMode = *ciMode
	c.RaceDetection = *raceDetection
	c.GenerateHTML = *generateHTML
	c.FailOnCriticalBlocks = *failOnCriticalBlocks
	c.ShowTestCounts = *showTestCounts

	return normalizeConfig(c), nil
}

func buildTestArgs(pkgs []string) []string {
	args := []string{"test"}
	args = append(args, pkgs...)
	args = append(args, "-json", "-covermode=atomic", "-coverprofile="+cfg.CoverProfile)
	if cfg.TestTimeout != "" {
		args = append(args, "-timeout="+cfg.TestTimeout)
	}
	if cfg.RaceDetection {
		args = append(args, "-race")
	}
	if cfg.BuildTags != "" {
		args = append(args, "-tags", cfg.BuildTags)
	}
	return args
}

// runTests executes go test and parses JSON output
func runTests() ([]PackageResult, map[string]int, map[string]int, int) {
	// Build package list
	pkgCmd := exec.Command("go", "list", "./...")
	pkgOutput, err := pkgCmd.Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing packages: %v\n", err)
		return nil, nil, nil, 1
	}

	var pkgs []string
	for _, pkg := range strings.Split(strings.TrimSpace(string(pkgOutput)), "\n") {
		if pkg == "" {
			continue
		}
		excluded := false
		for _, ex := range cfg.ExcludePackages {
			if ex != "" && strings.Contains(pkg, ex) {
				excluded = true
				break
			}
		}
		if !excluded {
			pkgs = append(pkgs, pkg)
		}
	}
	if len(pkgs) == 0 {
		fmt.Fprintln(os.Stderr, "Error listing packages: no packages selected")
		return nil, nil, nil, 1
	}

	args := buildTestArgs(pkgs)

	cmd := exec.Command("go", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating stdout pipe: %v\n", err)
		return nil, nil, nil, 1
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting tests: %v\n", err)
		return nil, nil, nil, 1
	}

	results, topLevelCounts, subTestCounts := parseTestOutput(stdout)

	exitCode := 0
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	return results, topLevelCounts, subTestCounts, exitCode
}

// parseTestOutput parses JSON output from go test
func parseTestOutput(r io.Reader) ([]PackageResult, map[string]int, map[string]int) {
	results := make(map[string]*PackageResult)
	topLevelCounts := make(map[string]int)
	subTestCounts := make(map[string]int)
	decoder := json.NewDecoder(r)

	// Track which packages we've printed and their start times
	printedPackages := make(map[string]bool)
	packageStartTimes := make(map[string]time.Time)

	// Buffer output per test, only print on failure
	// Key: "pkg/test" or just "pkg" for package-level output
	testOutputs := make(map[string][]string)

	for {
		var event TestEvent
		if err := decoder.Decode(&event); err != nil {
			if err == io.EOF {
				break
			}
			continue
		}

		pkg := strings.TrimPrefix(event.Package, cfg.ModulePrefix)

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
				fmt.Printf("[%s] Testing %s...\n", now.Format("15:04:05"), pkg)
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
				fmt.Printf("[%s] ✓ %s completed (%.2fs)\n", time.Now().Format("15:04:05"), pkg, elapsed.Seconds())
			}
		}

		// Buffer output for each test
		if event.Action == "output" {
			// Parse coverage/result lines
			if strings.HasPrefix(event.Output, "ok") {
				parsePackageResult(event.Output, results)
			} else if strings.HasPrefix(event.Output, "FAIL") {
				parsePackageResult(event.Output, results)
			} else if strings.HasPrefix(event.Output, "?") {
				parsePackageResult(event.Output, results)
			} else {
				// Buffer output for potential failure
				testOutputs[outputKey] = append(testOutputs[outputKey], event.Output)
			}
		}

		// On test failure, print buffered output
		if event.Action == "fail" && event.Test != "" {
			if outputs, ok := testOutputs[outputKey]; ok {
				fmt.Printf("\n=== FAIL: %s/%s ===\n", pkg, event.Test)
				for _, out := range outputs {
					fmt.Print(out)
				}
			}
			delete(testOutputs, outputKey)
		}

		// Clean up on pass
		if event.Action == "pass" && event.Test != "" {
			delete(testOutputs, outputKey)
		}
	}

	// Convert map to slice
	var resultSlice []PackageResult
	for _, r := range results {
		resultSlice = append(resultSlice, *r)
	}

	return resultSlice, topLevelCounts, subTestCounts
}

// parsePackageResult parses a single package result line
func parsePackageResult(line string, results map[string]*PackageResult) {
	line = strings.TrimSpace(line)
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return
	}

	status := fields[0]
	pkg := strings.TrimPrefix(fields[1], cfg.ModulePrefix)
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

// printPackageSummary prints the package coverage summary table
// Returns true if any package is below threshold (CRITICAL)
func printPackageSummary(results []PackageResult, topLevelCounts, subTestCounts map[string]int) bool {
	hasCritical := false

	fmt.Println("Package coverage summary:")
	if cfg.ShowTestCounts {
		fmt.Printf("%-3s %-40s %-10s %-7s %s\n", "OK", "PACKAGE", "DURATION", "TESTS", "COVERAGE")
	} else {
		fmt.Printf("%-3s %-40s %-10s %s\n", "OK", "PACKAGE", "DURATION", "COVERAGE")
	}
	fmt.Println(strings.Repeat("-", 80))

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
					fmt.Printf("::error::%-3s %-40s %-10s %s (CRITICAL: < %.0f%%)\n",
						r.Status, r.Name, duration, coverageStr, cfg.ThresholdPackage)
				} else {
					coverageStr = fmt.Sprintf("%s%s%s (CRITICAL: < %.0f%%)", ColorRed, r.CoverageStr, ColorReset, cfg.ThresholdPackage)
					fmt.Printf("%-3s %-40s %-10s %-7d %s\n", r.Status, r.Name, duration, total, coverageStr)
				}
			} else {
				if cfg.ShowTestCounts {
					fmt.Printf("%-3s %-40s %-10s %-7d %s\n", r.Status, r.Name, duration, total, coverageStr)
				} else {
					fmt.Printf("%-3s %-40s %-10s %s\n", r.Status, r.Name, duration, coverageStr)
				}
			}
		}
	}

	// Print failed packages
	for _, r := range results {
		if r.Status == "FAIL" {
			if cfg.CIMode {
				fmt.Printf("::error::%-3s %-40s %s\n", r.Status, r.Name, "[FAILED]")
			} else {
				fmt.Printf("%s%-3s %-40s %s%s\n", ColorRed, r.Status, r.Name, "[FAILED]", ColorReset)
			}
			hasCritical = true
		}
	}

	// Print skipped packages
	for _, r := range results {
		if r.Status == "?" {
			fmt.Printf("%-3s %-40s %s\n", r.Status, r.Name, "[no test files]")
		}
	}

	return hasCritical
}

// getFunctionCoverage runs go tool cover -func and parses output
func getFunctionCoverage() []FuncCoverage {
	cmd := exec.Command("go", "tool", "cover", "-func="+cfg.CoverProfile)
	output, err := cmd.Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting function coverage: %v\n", err)
		return nil
	}

	return parseFunctionCoverageOutput(string(output), cfg)
}

func parseFunctionCoverageOutput(output string, c Config) []FuncCoverage {
	var funcs []FuncCoverage
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimPrefix(line, c.ModulePrefix)

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
		// Also skip functions ending in "ForTest" — test helpers in non-test files
		if strings.HasSuffix(fields[1], "ForTest") {
			excluded = true
		}
		// Skip excluded functions (nocov: pebble batch error paths)
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

// printFunctionCoverage prints function coverage details
// Returns (hasCritical, totalWidth) where totalWidth is for alignment
func printFunctionCoverage(funcs []FuncCoverage) (bool, int) {
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

	fmt.Printf("\nFunction coverage details (excluding >= %.0f%%):\n", cfg.ThresholdPrint)
	fmt.Printf("%-*s %-*s %s\n", maxLocWidth, "LOCATION", maxFuncWidth, "FUNCTION", "COVERAGE")
	fmt.Println(strings.Repeat("-", totalWidth))

	fmt.Printf("... %d more...\n", aboveThreshold)

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
				fmt.Printf("::error file=%s,line=%s::%-*s %-*s %s (CRITICAL < %.0f%%)\n",
					file, line, maxLocWidth, f.Location, maxFuncWidth, f.Function, covStr, cfg.ThresholdFunc)
			} else {
				covStr = fmt.Sprintf("%s%.1f%%%s (CRITICAL: < %.0f%%)", ColorRed, f.Coverage, ColorReset, cfg.ThresholdFunc)
				fmt.Printf("%-*s %-*s %s\n", maxLocWidth, f.Location, maxFuncWidth, f.Function, covStr)
			}
		} else {
			fmt.Printf("%-*s %-*s %s\n", maxLocWidth, f.Location, maxFuncWidth, f.Function, covStr)
		}
	}

	fmt.Println(strings.Repeat("-", totalWidth))
	return hasCritical, totalWidth
}

// printTotal prints total coverage and checks threshold
// Returns true if total is below threshold (CRITICAL)
func printTotal(width int) bool {
	totalCov := getTotalCoverage()
	isCritical := totalCov < cfg.ThresholdTotal

	totalStr := fmt.Sprintf("%.1f%%", totalCov)
	labelWidth := width - 10 // Leave space for coverage value
	if labelWidth < 10 {
		labelWidth = 10
	}

	if isCritical {
		if cfg.CIMode {
			fmt.Printf("::error::%-*s %s (CRITICAL: < %.0f%%)\n", labelWidth, "TOTAL", totalStr, cfg.ThresholdTotal)
		} else {
			totalStr = fmt.Sprintf("%s%.1f%%%s (CRITICAL: < %.0f%%)", ColorRed, totalCov, ColorReset, cfg.ThresholdTotal)
			fmt.Printf("%-*s %s\n", labelWidth, "TOTAL", totalStr)
		}
	} else {
		fmt.Printf("%-*s %s\n", labelWidth, "TOTAL", totalStr)
	}
	fmt.Println(strings.Repeat("-", width))

	return isCritical
}

// getTotalCoverage calculates total coverage from coverage profile
func getTotalCoverage() float64 {
	cmd := exec.Command("go", "tool", "cover", "-func="+cfg.CoverProfile)
	output, err := cmd.Output()
	if err != nil {
		return 0
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "total:") {
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				covStr := strings.TrimSuffix(fields[len(fields)-1], "%")
				cov, _ := strconv.ParseFloat(covStr, 64)
				return cov
			}
		}
	}
	return 0
}

// printStatistics prints coverage statistics
func printStatistics(funcs []FuncCoverage, topLevelCounts, subTestCounts map[string]int) {
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

	fmt.Println("Statistics:")
	fmt.Printf("Functions with 100%% coverage: %d\n", count100)
	fmt.Printf("Functions with 95%%-100%% coverage: %d\n", count95_100)
	fmt.Printf("Functions with 85%%-95%% coverage: %d\n", count85_95)
	fmt.Printf("Functions with <85%% coverage: %d\n", countLt85)

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
		fmt.Printf("Total tests: %d (including subtests, %d top-level)\n", totalTopLevel+totalSubTests, totalTopLevel)
	}
}

// generateHTMLReport generates an HTML coverage report
func generateHTMLReport() {
	if dir := filepath.Dir(cfg.HTMLPath); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "Error creating HTML report directory: %v\n", err)
			return
		}
	}
	cmd := exec.Command("go", "tool", "cover", "-html="+cfg.CoverProfile, "-o", cfg.HTMLPath)
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error generating HTML report: %v\n", err)
	}
}

// analyzeUncoveredBlocks parses coverage file and analyzes uncovered blocks
// Returns true if any CRITICAL blocks found
func analyzeUncoveredBlocks() bool {
	file, err := os.Open(cfg.CoverProfile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening coverage file: %v\n", err)
		return false
	}
	defer file.Close()

	var blocks []Block
	scanner := bufio.NewScanner(file)

	// Skip mode line
	if scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "mode:") {
			if b, ok := parseLine(line, cfg); ok {
				blocks = append(blocks, b)
			}
		}
	}

	for scanner.Scan() {
		if b, ok := parseLine(scanner.Text(), cfg); ok {
			blocks = append(blocks, b)
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Error reading coverage file: %v\n", err)
		return false
	}

	merged := mergeBlocks(blocks)

	// Analyze blocks using AST
	fileCache := NewFileCache()
	astCache := NewASTCache()
	for i := range merged {
		AnalyzeBlockWithAST(&merged[i], astCache, fileCache)
	}

	// Sort by Level (CRITICAL > HIGH > MEDIUM > LOW) then by NumLines descending
	levelWeight := map[string]int{
		"CRITICAL": 4,
		"HIGH":     3,
		"MEDIUM":   2,
		"LOW":      1,
	}

	sort.Slice(merged, func(i, j int) bool {
		w1 := levelWeight[merged[i].Level]
		w2 := levelWeight[merged[j].Level]
		if w1 != w2 {
			return w1 > w2
		}
		if merged[i].EffectiveLines != merged[j].EffectiveLines {
			return merged[i].EffectiveLines > merged[j].EffectiveLines
		}
		return merged[i].NumLines > merged[j].NumLines
	})

	// Check if any CRITICAL blocks exist
	hasCritical := false
	for _, b := range merged {
		if b.Level == "CRITICAL" {
			hasCritical = true
			break
		}
	}

	// Print output
	maxLocWidth := calculateMaxLocWidth(merged)
	printUncoveredHeader(maxLocWidth, false)
	printBlocks(merged, maxLocWidth, cfg.UncoveredLimit)

	return hasCritical
}
