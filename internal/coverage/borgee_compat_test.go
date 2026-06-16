package coverage

import "testing"

func TestConfigFromEnvBorgeeCoverageOverrides(t *testing.T) {
	t.Setenv("CI", "true")
	t.Setenv("BUILD_TAGS", "sqlite_fts5 race_heavy")
	t.Setenv("GENERATE_HTML", "false")
	t.Setenv("RACE_DETECTION", "false")
	t.Setenv("FAIL_ON_CRITICAL_BLOCKS", "false")

	cfg, err := ConfigFromEnv(nil)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.BuildTags != "sqlite_fts5 race_heavy" {
		t.Fatalf("BuildTags = %q", cfg.BuildTags)
	}
	if cfg.GenerateHTML {
		t.Fatal("GenerateHTML = true, want false")
	}
	if cfg.RaceDetection {
		t.Fatal("RaceDetection = true, want false")
	}
	if cfg.FailOnCriticalBlocks {
		t.Fatal("FailOnCriticalBlocks = true, want false")
	}
}

func TestBuildTestArgsIncludesTagsAndNoRaceOverride(t *testing.T) {
	cfg := Config{
		CoverProfile:  "coverage.out",
		TestTimeout:   "180s",
		BuildTags:     "sqlite_fts5 race_heavy",
		RaceDetection: false,
	}

	args := buildTestArgs(cfg, []string{"./internal/api"})
	want := []string{
		"test", "./internal/api", "-json", "-covermode=atomic", "-coverprofile=coverage.out",
		"-timeout=180s", "-tags", "sqlite_fts5 race_heavy",
	}
	if len(args) != len(want) {
		t.Fatalf("args length = %d, want %d: %#v", len(args), len(want), args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args[%d] = %q, want %q; full args %#v", i, args[i], want[i], args)
		}
	}
}
