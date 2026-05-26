package coverage

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

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
	Packages []string `toml:"packages"`
	Files    []string `toml:"files"`
	Funcs    []string `toml:"funcs"`
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
	if c.ProjectName == "" {
		c.ProjectName = projectNameFromModule(strings.TrimSuffix(c.ModulePrefix, "/"))
	}
	if c.CoverProfile == "" {
		c.CoverProfile = "/tmp/coverage.out"
	}
	if c.HTMLPath == "" {
		c.HTMLPath = defaultHTMLPath(c.ProjectName)
	}
	if c.TestTimeout == "" {
		c.TestTimeout = "15m"
	}
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

func detectModulePath() string {
	cmd := exec.Command("go", "list", "-m")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
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
