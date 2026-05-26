package coverage

import (
	"os/exec"
	"path"
	"strings"
)

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
