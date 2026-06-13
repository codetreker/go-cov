package coverage

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

// captureStdout redirects os.Stdout for the duration of fn and returns what was written.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe(): %v", err)
	}
	os.Stdout = w

	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()

	fn()

	_ = w.Close()
	os.Stdout = old
	return <-done
}

// A package that fails to compile must surface the compiler error, not just "FAILED".
func TestParseTestOutputPrintsBuildFailureDetails(t *testing.T) {
	cfg = Config{ModulePrefix: "covdemo/"}

	input := strings.Join([]string{
		`{"ImportPath":"covdemo/sub [covdemo/sub.test]","Action":"build-output","Output":"# covdemo/sub [covdemo/sub.test]\n"}`,
		`{"ImportPath":"covdemo/sub [covdemo/sub.test]","Action":"build-output","Output":"sub/sub.go:4:9: undefined: notAFunction\n"}`,
		`{"ImportPath":"covdemo/sub [covdemo/sub.test]","Action":"build-fail"}`,
		`{"Action":"start","Package":"covdemo/sub"}`,
		`{"Action":"output","Package":"covdemo/sub","Output":"FAIL\tcovdemo/sub [build failed]\n"}`,
		`{"Action":"fail","Package":"covdemo/sub","FailedBuild":"covdemo/sub [covdemo/sub.test]"}`,
	}, "\n")

	out := captureStdout(t, func() {
		parseTestOutput(strings.NewReader(input))
	})

	if !strings.Contains(out, "undefined: notAFunction") {
		t.Fatalf("build failure detail missing from output:\n%s", out)
	}
}

// Package-level failure output (e.g. a panic outside any single test) must be surfaced.
func TestParseTestOutputPrintsPackageLevelFailureDetails(t *testing.T) {
	cfg = Config{ModulePrefix: "covdemo/"}

	input := strings.Join([]string{
		`{"Action":"start","Package":"covdemo"}`,
		`{"Action":"output","Package":"covdemo","Output":"panic: boom from a goroutine\n"}`,
		`{"Action":"output","Package":"covdemo","Output":"\tgoroutine stack trace line\n"}`,
		`{"Action":"output","Package":"covdemo","Output":"FAIL\tcovdemo\t0.005s\n"}`,
		`{"Action":"fail","Package":"covdemo"}`,
	}, "\n")

	out := captureStdout(t, func() {
		parseTestOutput(strings.NewReader(input))
	})

	if !strings.Contains(out, "boom from a goroutine") {
		t.Fatalf("package-level failure detail missing from output:\n%s", out)
	}
}
