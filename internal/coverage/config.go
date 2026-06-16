package coverage

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
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
	ModulePrefix         string   // Primary Go module import prefix stripped from display paths
	ModulePrefixes       []string // All main-module prefixes to strip (covers Go workspaces); longest match wins
	ProjectName          string   // Project name used for default local HTML output
	ExcludePackages      []string // Packages to exclude by substring
	SkipResultPackages   []string // Package display names to skip from summaries by substring
	ExcludeFiles         []string // File path substrings to exclude from function/block reports
	ExcludeFuncs         []string // Function names to exclude from coverage threshold
	ExcludeFuncSuffixes  []string // Function name suffixes to exclude (e.g. "ForTest" helpers)
	CoverProfile         string   // Coverage profile path
	HTMLPath             string   // HTML coverage report output path
	TestTimeout          string   // go test timeout value
	BuildTags            string   // Build tags passed to go test
	GenerateHTML         bool     // Generate local HTML coverage report when not in CI mode
	FailOnCriticalBlocks bool     // In CI mode, fail on AST critical uncovered blocks
	UncoveredLimit       int      // Max uncovered blocks to show
	ShowTestCounts       bool     // Show TESTS column in package summary
	ColorEnabled         bool     // Emit ANSI color escapes (false when NO_COLOR is set or stdout is not a TTY)
}

// stripModulePrefix removes the longest matching main-module import-path prefix
// from s so displayed package and file paths are relative to their module root.
// It falls back to ModulePrefix when ModulePrefixes is unset, and returns s
// unchanged when nothing matches (e.g. no module detected, or a third-party path).
func (c Config) stripModulePrefix(s string) string {
	prefixes := c.ModulePrefixes
	if len(prefixes) == 0 && c.ModulePrefix != "" {
		prefixes = []string{c.ModulePrefix}
	}
	best := ""
	for _, p := range prefixes {
		if p != "" && len(p) > len(best) && strings.HasPrefix(s, p) {
			best = p
		}
	}
	return strings.TrimPrefix(s, best)
}

// DefaultConfig returns conservative defaults suitable for most Go projects.
func DefaultConfig() Config {
	modulePaths := detectModulePaths()
	primaryModule := ""
	if len(modulePaths) > 0 {
		primaryModule = modulePaths[0]
	}
	projectName := projectNameFromModule(primaryModule)

	c := Config{
		ThresholdFunc:        80.0,
		ThresholdPackage:     85.0,
		ThresholdPrint:       85.0,
		ThresholdTotal:       90.0,
		CIMode:               os.Getenv("CI") == "true",
		RaceDetection:        false,
		ModulePrefix:         normalizeModulePrefix(primaryModule),
		ModulePrefixes:       normalizeModulePrefixes(modulePaths),
		ProjectName:          projectName,
		ExcludePackages:      nil,
		SkipResultPackages:   nil,
		ExcludeFiles:         nil,
		ExcludeFuncs:         nil,
		ExcludeFuncSuffixes:  []string{"ForTest"},
		CoverProfile:         "", // empty => a unique temp file is allocated per run
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
	configPath, explicitConfig, remainingArgs, err := extractConfigArg(args)
	if err != nil {
		return Config{}, err
	}
	if configPath == "" {
		configPath = ".go-cov.toml"
	}
	if err := applyConfigFile(&c, configPath, explicitConfig); err != nil {
		return Config{}, err
	}

	// Override from environment first; flags win below.
	if v := os.Getenv("COVERPROFILE"); v != "" {
		c.CoverProfile = v
	}
	if v := os.Getenv("MODULE_PREFIX"); v != "" {
		c.ModulePrefix = normalizeModulePrefix(v)
		c.ModulePrefixes = []string{c.ModulePrefix} // explicit prefix overrides auto-detection
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
	if v := os.Getenv("EXCLUDE_FUNC_SUFFIXES"); v != "" {
		c.ExcludeFuncSuffixes = splitCSV(v)
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
	fs.String("config", configPath, "config file path")
	modulePrefix := fs.String("module-prefix", c.ModulePrefix, "module import prefix to strip from output paths")
	projectName := fs.String("project", c.ProjectName, "project name used for default local HTML output")
	coverProfile := fs.String("coverprofile", c.CoverProfile, "coverage profile path")
	htmlPath := fs.String("html-out", c.HTMLPath, "HTML coverage report output path")
	excludePackages := fs.String("exclude-packages", joinCSV(c.ExcludePackages), "comma-separated package substrings to exclude")
	skipResultPackages := fs.String("skip-result-packages", joinCSV(c.SkipResultPackages), "comma-separated displayed package substrings to omit from summaries")
	excludeFiles := fs.String("exclude-files", joinCSV(c.ExcludeFiles), "comma-separated file substrings to exclude")
	excludeFuncs := fs.String("exclude-funcs", joinCSV(c.ExcludeFuncs), "comma-separated function name substrings to exclude")
	excludeFuncSuffixes := fs.String("exclude-func-suffixes", joinCSV(c.ExcludeFuncSuffixes), "comma-separated function name suffixes to exclude")
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

	if err := fs.Parse(remainingArgs); err != nil {
		return Config{}, err
	}
	projectFlagSet := false
	htmlFlagSet := false
	modulePrefixFlagSet := false
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "project":
			projectFlagSet = true
		case "html-out":
			htmlFlagSet = true
		case "module-prefix":
			modulePrefixFlagSet = true
		}
	})

	c.ModulePrefix = normalizeModulePrefix(*modulePrefix)
	if modulePrefixFlagSet {
		c.ModulePrefixes = []string{c.ModulePrefix} // explicit prefix overrides auto-detection
	}
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
	c.ExcludeFuncSuffixes = splitCSV(*excludeFuncSuffixes)
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

type fileConfig struct {
	ProjectName    *string            `toml:"project"`
	ModulePrefix   *string            `toml:"module_prefix"`
	Thresholds     fileThresholds     `toml:"thresholds"`
	Test           fileTest           `toml:"test"`
	Exclude        fileExclude        `toml:"exclude"`
	HTML           fileHTML           `toml:"html"`
	CriticalBlocks fileCriticalBlocks `toml:"critical_blocks"`
}

type fileThresholds struct {
	Total    *float64 `toml:"total"`
	Function *float64 `toml:"function"`
	Package  *float64 `toml:"package"`
	Print    *float64 `toml:"print"`
}

type fileTest struct {
	Timeout *string  `toml:"timeout"`
	Race    *bool    `toml:"race"`
	Tags    []string `toml:"tags"`
}

type fileExclude struct {
	Packages     []string `toml:"packages"`
	Files        []string `toml:"files"`
	Funcs        []string `toml:"funcs"`
	FuncSuffixes []string `toml:"func_suffixes"`
}

type fileHTML struct {
	Enabled *bool   `toml:"enabled"`
	Path    *string `toml:"path"`
}

type fileCriticalBlocks struct {
	Fail *bool `toml:"fail"`
}

func normalizeConfig(c Config) Config {
	c.ModulePrefix = normalizeModulePrefix(c.ModulePrefix)
	c.ModulePrefixes = normalizeModulePrefixes(c.ModulePrefixes)
	if c.ProjectName == "" {
		c.ProjectName = projectNameFromModule(strings.TrimSuffix(c.ModulePrefix, "/"))
	}
	if c.HTMLPath == "" {
		c.HTMLPath = defaultHTMLPath(c.ProjectName)
	}
	if c.TestTimeout == "" {
		c.TestTimeout = "15m"
	}
	c.ColorEnabled = detectColorEnabled()
	return c
}

func normalizeModulePrefix(modulePath string) string {
	modulePath = strings.TrimSpace(modulePath)
	if modulePath == "" {
		return ""
	}
	return strings.TrimRight(modulePath, "/") + "/"
}

func defaultHTMLPath(projectName string) string {
	projectName = strings.TrimSpace(projectName)
	if projectName == "" {
		return "test_coverage.html"
	}
	return "." + projectName + "/test_coverage.html"
}

func projectNameFromModule(modulePath string) string {
	modulePath = strings.Trim(strings.TrimSpace(modulePath), "/")
	if modulePath == "" {
		return ""
	}
	return path.Base(modulePath)
}

// detectModulePaths returns the import path of every main module. In a Go
// workspace `go list -m` prints one module per line, so callers must be ready
// for more than one. Returns nil when detection fails (e.g. GOPATH mode).
func detectModulePaths() []string {
	cmd := exec.Command("go", "list", "-m")
	output, err := cmd.Output()
	if err != nil {
		return nil
	}
	return parseModuleList(string(output))
}

// parseModuleList extracts module import paths from `go list -m` output, one per
// line. Blank lines and Go's "go: ..." toolchain notices are ignored.
func parseModuleList(output string) []string {
	var mods []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "go:") {
			continue
		}
		mods = append(mods, line)
	}
	return mods
}

// normalizeModulePrefixes normalizes each module path into a strippable prefix
// (trailing slash) and drops empties.
func normalizeModulePrefixes(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if np := normalizeModulePrefix(p); np != "" {
			out = append(out, np)
		}
	}
	return out
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func joinCSV(values []string) string {
	return strings.Join(values, ",")
}

func extractConfigArg(args []string) (string, bool, []string, error) {
	remaining := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--config" {
			if i+1 >= len(args) {
				return "", false, nil, fmt.Errorf("--config requires a path")
			}
			return args[i+1], true, append(remaining, args[i+2:]...), nil
		}
		if strings.HasPrefix(arg, "--config=") {
			return strings.TrimPrefix(arg, "--config="), true, append(remaining, args[i+1:]...), nil
		}
		remaining = append(remaining, arg)
	}
	return "", false, remaining, nil
}

func applyConfigFile(c *Config, path string, required bool) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) && !required {
			return nil
		}
		return fmt.Errorf("read config %s: %w", path, err)
	}

	var fc fileConfig
	if err := toml.Unmarshal(data, &fc); err != nil {
		return fmt.Errorf("parse config %s: %w", path, err)
	}
	mergeFileConfig(c, fc)
	return nil
}

func mergeFileConfig(c *Config, fc fileConfig) {
	if fc.ProjectName != nil {
		c.ProjectName = *fc.ProjectName
		c.HTMLPath = defaultHTMLPath(*fc.ProjectName)
	}
	if fc.ModulePrefix != nil {
		c.ModulePrefix = normalizeModulePrefix(*fc.ModulePrefix)
		c.ModulePrefixes = []string{c.ModulePrefix} // explicit prefix overrides auto-detection
	}
	if fc.Thresholds.Total != nil {
		c.ThresholdTotal = *fc.Thresholds.Total
	}
	if fc.Thresholds.Function != nil {
		c.ThresholdFunc = *fc.Thresholds.Function
	}
	if fc.Thresholds.Package != nil {
		c.ThresholdPackage = *fc.Thresholds.Package
	}
	if fc.Thresholds.Print != nil {
		c.ThresholdPrint = *fc.Thresholds.Print
	}
	if fc.Test.Timeout != nil {
		c.TestTimeout = *fc.Test.Timeout
	}
	if fc.Test.Race != nil {
		c.RaceDetection = *fc.Test.Race
	}
	if fc.Test.Tags != nil {
		c.BuildTags = strings.Join(fc.Test.Tags, " ")
	}
	if fc.Exclude.Packages != nil {
		c.ExcludePackages = fc.Exclude.Packages
	}
	if fc.Exclude.Files != nil {
		c.ExcludeFiles = fc.Exclude.Files
	}
	if fc.Exclude.Funcs != nil {
		c.ExcludeFuncs = fc.Exclude.Funcs
	}
	if fc.Exclude.FuncSuffixes != nil {
		c.ExcludeFuncSuffixes = fc.Exclude.FuncSuffixes
	}
	if fc.HTML.Enabled != nil {
		c.GenerateHTML = *fc.HTML.Enabled
	}
	if fc.HTML.Path != nil {
		c.HTMLPath = *fc.HTML.Path
	}
	if fc.CriticalBlocks.Fail != nil {
		c.FailOnCriticalBlocks = *fc.CriticalBlocks.Fail
	}
}
