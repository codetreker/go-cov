// Package coverage provides a Go-based coverage analysis tool.
// It runs tests, collects coverage data, and generates comprehensive reports.
package coverage

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// Run executes the coverage workflow and returns a process-style exit code.
// Report output is written to out (the CLI passes os.Stdout); warnings and
// errors continue to go to os.Stderr.
func Run(out io.Writer, c Config) int {
	cfg := normalizeConfig(c)

	profile, cleanup, err := resolveCoverProfile(cfg.CoverProfile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating coverage profile: %v\n", err)
		return 1
	}
	cfg.CoverProfile = profile
	defer cleanup()

	// The deferred cleanup above does not run when the process is terminated by a
	// signal (the normal path exits via os.Exit in main, which also skips defers,
	// but Run returns there cleanly; an interrupt does not). Install a handler that
	// removes the temp profile before exiting so SIGINT/SIGTERM does not leak it.
	// os.Remove of an already-removed or explicitly configured file is harmless.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		cleanup()
		os.Exit(1)
	}()

	if len(cfg.ModulePrefixes) == 0 && cfg.ModulePrefix == "" {
		fmt.Fprintln(os.Stderr, "go-cov: no module path detected; displayed paths will not be shortened. Pass --module-prefix to set one.")
	}

	fmt.Fprintln(out)
	startTime := time.Now()
	fmt.Fprintf(out, "[%s] Starting test run...\n\n", startTime.Format("15:04:05"))

	// Run tests and collect results
	results, topLevelCounts, subTestCounts, exitCode := runTests(out, cfg)
	if exitCode != 0 && len(results) == 0 {
		return exitCode
	}

	testDuration := time.Since(startTime)
	fmt.Fprintf(out, "\n[%s] Test run completed in %.2fs\n", time.Now().Format("15:04:05"), testDuration.Seconds())
	fmt.Fprintln(out, strings.Repeat("=", 80))
	fmt.Fprintln(out)

	// Print package coverage summary
	hasCriticalPackage := printPackageSummary(out, cfg, results, topLevelCounts, subTestCounts)

	if exitCode != 0 {
		return exitCode
	}

	// Get function coverage data from a single `go tool cover -func` invocation,
	// deriving both the per-function list and the total from the same output.
	funcOutput, err := runFuncCoverage(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting function coverage: %v\n", err)
	}
	funcData := parseFunctionCoverageOutput(funcOutput, cfg)
	totalCov, totalKnown := parseTotalCoverageOutput(funcOutput)

	// Print function coverage details
	hasCriticalFunc, funcWidth := printFunctionCoverage(out, cfg, funcData)

	// Print total and check threshold
	hasCriticalTotal := printTotal(out, cfg, totalCov, totalKnown, funcWidth)

	// Print statistics
	printStatistics(out, cfg, funcData, topLevelCounts, subTestCounts)

	// Generate HTML report (only in local mode)
	if !cfg.CIMode && cfg.GenerateHTML {
		generateHTMLReport(cfg)
	}

	// Analyze uncovered blocks
	fmt.Fprintln(out)
	fmt.Fprintln(out, strings.Repeat("-", 90))
	hasCriticalBlocks := analyzeUncoveredBlocks(out, cfg)

	// In CI mode, exit with error if any CRITICAL issues
	if cfg.CIMode && (hasCriticalPackage || hasCriticalFunc || hasCriticalTotal || (cfg.FailOnCriticalBlocks && hasCriticalBlocks)) {
		return 1
	}

	return 0
}

// resolveCoverProfile decides which coverage profile path to use. An empty
// configured path means "pick a unique temp file under the OS temp dir", which
// keeps the profile off the source tree and avoids two concurrent runs clobbering
// the same file. The returned cleanup removes that temp file; for an explicitly
// configured path the cleanup is a no-op so the caller's file is left in place.
func resolveCoverProfile(configured string) (string, func(), error) {
	if configured != "" {
		return configured, func() {}, nil
	}
	f, err := os.CreateTemp("", "go-cov-*.out")
	if err != nil {
		return "", func() {}, err
	}
	name := f.Name()
	_ = f.Close()
	return name, func() { _ = os.Remove(name) }, nil
}

func buildTestArgs(cfg Config, pkgs []string) []string {
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
func runTests(out io.Writer, cfg Config) ([]PackageResult, map[string]int, map[string]int, int) {
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

	args := buildTestArgs(cfg, pkgs)

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

	results, topLevelCounts, subTestCounts := parseTestOutput(out, cfg, stdout)

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

// runFuncCoverage runs `go tool cover -func` once and returns its raw output.
// Both the per-function list and the "total:" line are derived from this single
// invocation, avoiding a redundant subprocess and re-parse of the same profile.
func runFuncCoverage(cfg Config) (string, error) {
	cmd := exec.Command("go", "tool", "cover", "-func="+cfg.CoverProfile)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(output), nil
}

// generateHTMLReport generates an HTML coverage report
func generateHTMLReport(cfg Config) {
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
func analyzeUncoveredBlocks(out io.Writer, cfg Config) bool {
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

	fileCache := NewFileCache()
	merged := mergeBlocks(blocks, fileCache)

	// Analyze blocks using AST
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
	printUncoveredHeader(out, maxLocWidth, false)
	printBlocks(out, merged, maxLocWidth, cfg.UncoveredLimit, cfg.ColorEnabled, cfg.CIMode)

	return hasCritical
}
