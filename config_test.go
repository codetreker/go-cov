package coverage

import "testing"

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
