package coverage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeModulePrefix(t *testing.T) {
	t.Parallel()

	got := normalizeModulePrefix("github.com/codetrek/haystack")
	if got != "github.com/codetrek/haystack/" {
		t.Fatalf("normalizeModulePrefix() = %q, want trailing slash", got)
	}

	got = normalizeModulePrefix("github.com/codetrek/haystack/")
	if got != "github.com/codetrek/haystack/" {
		t.Fatalf("normalizeModulePrefix() changed existing slash: %q", got)
	}
}

func TestDefaultHTMLPathUsesProjectName(t *testing.T) {
	t.Parallel()

	got := defaultHTMLPath("haystack")
	if got != ".haystack/test_coverage.html" {
		t.Fatalf("defaultHTMLPath() = %q", got)
	}

	got = defaultHTMLPath("")
	if got != "test_coverage.html" {
		t.Fatalf("defaultHTMLPath(empty) = %q", got)
	}
}

func TestConfigFromEnvProjectFlagUpdatesDefaultHTMLPath(t *testing.T) {
	t.Setenv("CI", "")

	cfg, err := ConfigFromEnv([]string{"--project", "haystack"})
	if err != nil {
		t.Fatal(err)
	}

	if cfg.ProjectName != "haystack" {
		t.Fatalf("ProjectName = %q", cfg.ProjectName)
	}
	if cfg.HTMLPath != ".haystack/test_coverage.html" {
		t.Fatalf("HTMLPath = %q", cfg.HTMLPath)
	}
}

func TestConfigFromEnvHTMLFlagWinsOverProjectDefault(t *testing.T) {
	t.Setenv("CI", "")

	cfg, err := ConfigFromEnv([]string{"--project", "haystack", "--html-out", "coverage.html"})
	if err != nil {
		t.Fatal(err)
	}

	if cfg.HTMLPath != "coverage.html" {
		t.Fatalf("HTMLPath = %q", cfg.HTMLPath)
	}
}

func TestConfigFromEnvLoadsDefaultConfigFile(t *testing.T) {
	dir := t.TempDir()
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	writeConfigFile(t, filepath.Join(dir, ".go-cov.toml"), `[thresholds]
total = 85
function = 50
package = 70
print = 85

[test]
timeout = "15m"
race = false
tags = ["sqlite_fts5", "race_heavy"]

[exclude]
packages = [
  "borgee-server/scripts/",
  "borgee-server/cmd",
  "borgee-server/internal/testutil",
  "borgee-server/internal/api/cm5stance",
  "borgee-server/internal/testutil/regression_suite",
]
files = ["internal/testutil/", "main.go"]
funcs = []

[html]
enabled = false
path = ".coverage/test_coverage.html"

[critical_blocks]
fail = false
`)

	cfg, err := ConfigFromEnv(nil)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.ThresholdTotal != 85 || cfg.ThresholdFunc != 50 || cfg.ThresholdPackage != 70 || cfg.ThresholdPrint != 85 {
		t.Fatalf("thresholds not loaded: %+v", cfg)
	}
	if cfg.TestTimeout != "15m" || cfg.RaceDetection || cfg.BuildTags != "sqlite_fts5 race_heavy" {
		t.Fatalf("test settings not loaded: %+v", cfg)
	}
	assertStringSlice(t, cfg.ExcludePackages, []string{
		"borgee-server/scripts/",
		"borgee-server/cmd",
		"borgee-server/internal/testutil",
		"borgee-server/internal/api/cm5stance",
		"borgee-server/internal/testutil/regression_suite",
	})
	assertStringSlice(t, cfg.ExcludeFiles, []string{"internal/testutil/", "main.go"})
	assertStringSlice(t, cfg.ExcludeFuncs, nil)
	if cfg.GenerateHTML || cfg.HTMLPath != ".coverage/test_coverage.html" || cfg.FailOnCriticalBlocks {
		t.Fatalf("html/critical_blocks settings not loaded: %+v", cfg)
	}
}

func TestConfigFromEnvStillSupportsModuleAndProjectMetadata(t *testing.T) {
	dir := t.TempDir()
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	writeConfigFile(t, filepath.Join(dir, ".go-cov.toml"), `project = "syntrix"
module_prefix = "github.com/syntrixbase/syntrix/"

[thresholds]
total = 85
`)

	cfg, err := ConfigFromEnv(nil)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.ProjectName != "syntrix" {
		t.Fatalf("ProjectName = %q", cfg.ProjectName)
	}
	if cfg.ModulePrefix != "github.com/syntrixbase/syntrix/" {
		t.Fatalf("ModulePrefix = %q", cfg.ModulePrefix)
	}
	if cfg.ThresholdTotal != 85 {
		t.Fatalf("ThresholdTotal = %v", cfg.ThresholdTotal)
	}
}

func TestConfigPrecedenceDefaultsFileEnvFlags(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "coverage.toml")
	writeConfigFile(t, configPath, `project = "from-file"

[thresholds]
total = 75

[exclude]
packages = ["file/pkg"]

[html]
path = "file.html"
enabled = false
`)
	t.Setenv("THRESHOLD_TOTAL", "80")
	t.Setenv("HTML_OUT", "env.html")
	t.Setenv("EXCLUDE_PACKAGES", "env/pkg")

	cfg, err := ConfigFromEnv([]string{"--config", configPath, "--threshold-total", "90", "--exclude-packages", "flag/pkg"})
	if err != nil {
		t.Fatal(err)
	}

	if cfg.ProjectName != "from-file" {
		t.Fatalf("ProjectName = %q", cfg.ProjectName)
	}
	if cfg.ThresholdTotal != 90 {
		t.Fatalf("ThresholdTotal = %v", cfg.ThresholdTotal)
	}
	if cfg.HTMLPath != "env.html" {
		t.Fatalf("HTMLPath = %q", cfg.HTMLPath)
	}
	assertStringSlice(t, cfg.ExcludePackages, []string{"flag/pkg"})
	if cfg.GenerateHTML {
		t.Fatal("GenerateHTML env/flags should not override file false in this test")
	}
}

func TestConfigFromEnvMissingExplicitConfigFileFails(t *testing.T) {
	_, err := ConfigFromEnv([]string{"--config", filepath.Join(t.TempDir(), "missing.toml")})
	if err == nil {
		t.Fatal("ConfigFromEnv accepted a missing explicit config file")
	}
}

func TestConfigFromEnvDefaultsAndOverridesFuncSuffixes(t *testing.T) {
	t.Setenv("CI", "")

	cfg, err := ConfigFromEnv(nil)
	if err != nil {
		t.Fatal(err)
	}
	assertStringSlice(t, cfg.ExcludeFuncSuffixes, []string{"ForTest"})

	cfg, err = ConfigFromEnv([]string{"--exclude-func-suffixes", "Mock,Fake"})
	if err != nil {
		t.Fatal(err)
	}
	assertStringSlice(t, cfg.ExcludeFuncSuffixes, []string{"Mock", "Fake"})
}

func writeConfigFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertStringSlice(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len(%v) = %d, want %d", got, len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("slice[%d] = %q, want %q; got %v", i, got[i], want[i], got)
		}
	}
}
