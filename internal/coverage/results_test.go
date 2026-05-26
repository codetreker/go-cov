package coverage

import "testing"

func TestParsePackageResultSkipsConfiguredResultPackages(t *testing.T) {
	cfg = Config{
		ModulePrefix:       "github.com/syntrixbase/syntrix/",
		SkipResultPackages: []string{"tests/"},
	}

	results := map[string]*PackageResult{}
	parsePackageResult("ok  github.com/syntrixbase/syntrix/tests/integration  0.100s  coverage: 10.0% of statements", results)

	if len(results) != 0 {
		t.Fatalf("got results for skipped package: %+v", results)
	}
}
