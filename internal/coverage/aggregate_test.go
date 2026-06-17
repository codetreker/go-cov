package coverage

import (
	"os"
	"path/filepath"
	"testing"
)

// aggregateCoverage must weight coverage by statement count (NumStmt), not by
// block count, and must report both an overall total and a per-package breakdown
// keyed by the file's package directory.
func TestAggregateCoverageWeightsByStatements(t *testing.T) {
	t.Parallel()

	blocks := []Block{
		{File: "a/x.go", NumStmt: 3, Count: 1}, // covered
		{File: "a/x.go", NumStmt: 1, Count: 0}, // uncovered
		{File: "b/y.go", NumStmt: 2, Count: 0}, // uncovered
		{File: "b/y.go", NumStmt: 2, Count: 5}, // covered
	}

	total, perPkg := aggregateCoverage(blocks)

	// Overall: covered 3+2=5 of total 3+1+2+2=8 => 62.5%.
	if pct, known := total.percent(); !known || pct != 62.5 {
		t.Fatalf("total = (%v, known=%v), want (62.5, true)", pct, known)
	}
	// Package a: 3 of 4 => 75.0%.
	if pct, known := perPkg["a"].percent(); !known || pct != 75.0 {
		t.Fatalf("pkg a = (%v, known=%v), want (75.0, true)", pct, known)
	}
	// Package b: 2 of 4 => 50.0%.
	if pct, known := perPkg["b"].percent(); !known || pct != 50.0 {
		t.Fatalf("pkg b = (%v, known=%v), want (50.0, true)", pct, known)
	}
}

// With no statements, coverage is "no data" (unknown), not a genuine 0%.
func TestAggregateCoverageEmptyIsUnknown(t *testing.T) {
	t.Parallel()

	total, _ := aggregateCoverage(nil)
	if pct, known := total.percent(); known {
		t.Fatalf("empty total = (%v, known=%v), want known=false", pct, known)
	}
}

// applyComputedPackageCoverage must overwrite each package's coverage with the
// recomputed (exclusion-aware) value so the summary matches the other dimensions.
func TestApplyComputedPackageCoverageOverrides(t *testing.T) {
	t.Parallel()

	results := []PackageResult{
		{Name: "a", Status: "ok", Coverage: 99.0, CoverageStr: "99.0%"},
		{Name: "b", Status: "ok", Coverage: 10.0, CoverageStr: "10.0%"},
	}
	perPkg := map[string]covAgg{
		"a": {covered: 1, total: 4}, // 25.0%
		// "b" intentionally absent: it must keep its original value.
	}

	applyComputedPackageCoverage(results, perPkg, Config{})

	if results[0].Coverage != 25.0 || results[0].CoverageStr != "25.0%" {
		t.Fatalf("pkg a not overridden: %+v", results[0])
	}
	if results[1].Coverage != 10.0 || results[1].CoverageStr != "10.0%" {
		t.Fatalf("pkg b should be unchanged when absent from computed map: %+v", results[1])
	}
}

// A module's root package is reported by `go test` under its full import path
// (e.g. "github.com/x/y"), which stripModulePrefix leaves unstripped, while its
// root files key under "" via packageOf. The override must still find it so an
// excluded root-level file is removed from the root package's coverage too.
func TestApplyComputedPackageCoverageOverridesRootPackage(t *testing.T) {
	t.Parallel()

	cfg := Config{ModulePrefix: "github.com/x/y/"}
	results := []PackageResult{
		{Name: "github.com/x/y", Status: "ok", Coverage: 99.0, CoverageStr: "99.0%"},
	}
	perPkg := map[string]covAgg{"": {covered: 1, total: 4}} // root files key under ""

	applyComputedPackageCoverage(results, perPkg, cfg)

	if results[0].Coverage != 25.0 || results[0].CoverageStr != "25.0%" {
		t.Fatalf("root package not overridden: %+v", results[0])
	}
}

// In a Go workspace several modules' root packages would all collapse to the ""
// key and collide; rather than apply a merged (wrong) number, the override must
// leave such a package untouched (a harmless miss that keeps the go-test value).
func TestApplyComputedPackageCoverageWorkspaceRootNotCollapsed(t *testing.T) {
	t.Parallel()

	cfg := Config{ModulePrefixes: []string{"mod1/", "mod2/"}}
	results := []PackageResult{
		{Name: "mod1", Status: "ok", Coverage: 77.0, CoverageStr: "77.0%"},
	}
	perPkg := map[string]covAgg{"": {covered: 1, total: 4}}

	applyComputedPackageCoverage(results, perPkg, cfg)

	if results[0].Coverage != 77.0 {
		t.Fatalf("workspace root package must not be overridden from the colliding \"\" key: %+v", results[0])
	}
}

// The core bug: excluding a FILE must remove its statements from the total and
// per-package coverage, not just from the function/block lists.
func TestLoadCoverageBlocksExcludesFileFromTotals(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	profile := filepath.Join(dir, "cov.out")
	// keep.go: 4 statements, all covered. skip.go: 6 statements, all uncovered.
	// Without exclusion the total is 4/10 = 40%; excluding skip.go it is 4/4 = 100%.
	content := "mode: atomic\n" +
		"mod/keep.go:1.1,2.10 4 1\n" +
		"mod/skip.go:1.1,5.10 6 0\n"
	if err := os.WriteFile(profile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := Config{CoverProfile: profile, ExcludeFiles: []string{"skip.go"}}
	blocks, err := loadCoverageBlocks(cfg, NewASTCache())
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 1 || blocks[0].File != "mod/keep.go" {
		t.Fatalf("excluded file leaked into blocks: %+v", blocks)
	}

	total, perPkg := aggregateCoverage(blocks)
	if pct, known := total.percent(); !known || pct != 100.0 {
		t.Fatalf("total with skip.go excluded = (%v, known=%v), want (100, true)", pct, known)
	}
	if pct, _ := perPkg["mod"].percent(); pct != 100.0 {
		t.Fatalf("pkg mod with skip.go excluded = %v, want 100", pct)
	}
}

// Excluding a FUNCTION by name must remove its statements from the total/package
// coverage too. This requires attributing profile blocks to their enclosing
// function via the source AST.
func TestLoadCoverageBlocksExcludesFunctionFromTotals(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	src := filepath.Join(dir, "sample.go")
	// Line layout (1-based):
	//   1 package sample
	//   2 (blank)
	//   3 func Keep() int {
	//   4     return 1
	//   5 }
	//   6 (blank)
	//   7 func Drop() int {
	//   8     return 2
	//   9 }
	source := "package sample\n\nfunc Keep() int {\n\treturn 1\n}\n\nfunc Drop() int {\n\treturn 2\n}\n"
	if err := os.WriteFile(src, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	profile := filepath.Join(dir, "cov.out")
	// Keep's body block (covered) starts on line 3; Drop's body block (uncovered)
	// starts on line 7. The profile path must match the source path so the AST
	// can be loaded for function attribution.
	content := "mode: atomic\n" +
		src + ":3.16,5.2 1 1\n" +
		src + ":7.16,9.2 1 0\n"
	if err := os.WriteFile(profile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := Config{CoverProfile: profile, ExcludeFuncs: []string{"Drop"}}
	blocks, err := loadCoverageBlocks(cfg, NewASTCache())
	if err != nil {
		t.Fatal(err)
	}

	if len(blocks) != 1 || blocks[0].StartLine != 3 {
		t.Fatalf("Drop's block was not excluded: %+v", blocks)
	}
	total, _ := aggregateCoverage(blocks)
	// Only Keep remains: 1/1 = 100%. Without exclusion it would be 1/2 = 50%.
	if pct, known := total.percent(); !known || pct != 100.0 {
		t.Fatalf("total with Drop excluded = (%v, known=%v), want (100, true)", pct, known)
	}
}

// Coverage profiles can carry the same block more than once (e.g. atomic-mode
// merges across test binaries). go-cov must merge duplicates the way
// `go tool cover` does — summing the execution counts and counting the block's
// statements exactly once — rather than weighting a duplicated block more.
func TestLoadCoverageBlocksMergesDuplicateBlocks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	profile := filepath.Join(dir, "cov.out")
	// Block (1.1,2.5) appears twice: uncovered then covered. Summed count is 1
	// (covered). Block (3.1,4.5) is distinct and uncovered.
	content := "mode: atomic\n" +
		"mod/a.go:1.1,2.5 3 0\n" +
		"mod/a.go:1.1,2.5 3 1\n" +
		"mod/a.go:3.1,4.5 2 0\n"
	if err := os.WriteFile(profile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	blocks, err := loadCoverageBlocks(Config{CoverProfile: profile}, NewASTCache())
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 2 {
		t.Fatalf("duplicate block not merged: got %d blocks, want 2: %+v", len(blocks), blocks)
	}

	total, _ := aggregateCoverage(blocks)
	// Covered statements = 3 (the merged, now-covered block); total = 3+2 = 5.
	// Double-counting the duplicate would wrongly give 3/8 = 37.5%.
	if pct, known := total.percent(); !known || pct != 60.0 {
		t.Fatalf("total = (%v, known=%v), want (60, true)", pct, known)
	}
}
