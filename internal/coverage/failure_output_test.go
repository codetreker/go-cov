package coverage

import (
	"bytes"
	"strings"
	"testing"
)

// A package that fails to compile must surface the compiler error, not just "FAILED".
func TestParseTestOutputPrintsBuildFailureDetails(t *testing.T) {
	cfg := Config{ModulePrefix: "covdemo/"}

	input := strings.Join([]string{
		`{"ImportPath":"covdemo/sub [covdemo/sub.test]","Action":"build-output","Output":"# covdemo/sub [covdemo/sub.test]\n"}`,
		`{"ImportPath":"covdemo/sub [covdemo/sub.test]","Action":"build-output","Output":"sub/sub.go:4:9: undefined: notAFunction\n"}`,
		`{"ImportPath":"covdemo/sub [covdemo/sub.test]","Action":"build-fail"}`,
		`{"Action":"start","Package":"covdemo/sub"}`,
		`{"Action":"output","Package":"covdemo/sub","Output":"FAIL\tcovdemo/sub [build failed]\n"}`,
		`{"Action":"fail","Package":"covdemo/sub","FailedBuild":"covdemo/sub [covdemo/sub.test]"}`,
	}, "\n")

	var buf bytes.Buffer
	parseTestOutput(&buf, cfg, strings.NewReader(input))
	out := buf.String()

	if !strings.Contains(out, "undefined: notAFunction") {
		t.Fatalf("build failure detail missing from output:\n%s", out)
	}
}

// In CI mode a build failure must become a GitHub Actions error annotation
// pinned to the offending file and line.
func TestParseTestOutputEmitsCIErrorAnnotationForBuildFailure(t *testing.T) {
	cfg := Config{ModulePrefix: "covdemo/", CIMode: true}

	input := strings.Join([]string{
		`{"ImportPath":"covdemo/sub [covdemo/sub.test]","Action":"build-output","Output":"# covdemo/sub [covdemo/sub.test]\n"}`,
		`{"ImportPath":"covdemo/sub [covdemo/sub.test]","Action":"build-output","Output":"sub/sub.go:4:9: undefined: notAFunction\n"}`,
		`{"ImportPath":"covdemo/sub [covdemo/sub.test]","Action":"build-fail"}`,
		`{"Action":"start","Package":"covdemo/sub"}`,
		`{"Action":"output","Package":"covdemo/sub","Output":"FAIL\tcovdemo/sub [build failed]\n"}`,
		`{"Action":"fail","Package":"covdemo/sub","FailedBuild":"covdemo/sub [covdemo/sub.test]"}`,
	}, "\n")

	var buf bytes.Buffer
	parseTestOutput(&buf, cfg, strings.NewReader(input))
	out := buf.String()

	want := "::error file=sub/sub.go,line=4::undefined: notAFunction"
	if !strings.Contains(out, want) {
		t.Fatalf("missing CI error annotation %q in output:\n%s", want, out)
	}
}

// Package-level failure output (e.g. a panic outside any single test) must be surfaced.
func TestParseTestOutputPrintsPackageLevelFailureDetails(t *testing.T) {
	cfg := Config{ModulePrefix: "covdemo/"}

	input := strings.Join([]string{
		`{"Action":"start","Package":"covdemo"}`,
		`{"Action":"output","Package":"covdemo","Output":"panic: boom from a goroutine\n"}`,
		`{"Action":"output","Package":"covdemo","Output":"\tgoroutine stack trace line\n"}`,
		`{"Action":"output","Package":"covdemo","Output":"FAIL\tcovdemo\t0.005s\n"}`,
		`{"Action":"fail","Package":"covdemo"}`,
	}, "\n")

	var buf bytes.Buffer
	parseTestOutput(&buf, cfg, strings.NewReader(input))
	out := buf.String()

	if !strings.Contains(out, "boom from a goroutine") {
		t.Fatalf("package-level failure detail missing from output:\n%s", out)
	}
}
