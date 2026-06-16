package coverage

import (
	"strings"
	"testing"
	"time"
)

// A stray non-JSON line in the go test -json stream must not stall parsing:
// the prior json.Decoder-based loop would spin forever because the decoder
// never advances past a malformed token. parseTestOutput now reads line by
// line, so it must skip the junk line, parse the surrounding valid events, and
// return.
func TestParseTestOutputSkipsNonJSONLineAndReturns(t *testing.T) {
	cfg := Config{ModulePrefix: "covdemo/"}

	input := strings.Join([]string{
		`{"Action":"run","Package":"covdemo/sub","Test":"TestA"}`,
		`this is not JSON and must be skipped without hanging`,
		`{"Action":"run","Package":"covdemo/sub","Test":"TestA/case1"}`,
		`{"Action":"output","Package":"covdemo/sub","Output":"ok  \tcovdemo/sub\t0.010s\tcoverage: 75.0% of statements\n"}`,
		`{"Action":"pass","Package":"covdemo/sub"}`,
	}, "\n")

	type parsed struct {
		results        []PackageResult
		topLevelCounts map[string]int
		subTestCounts  map[string]int
	}

	done := make(chan parsed, 1)
	go func() {
		var p parsed
		out := captureStdout(t, func() {
			p.results, p.topLevelCounts, p.subTestCounts = parseTestOutput(cfg, strings.NewReader(input))
		})
		_ = out
		done <- p
	}()

	select {
	case p := <-done:
		if got := p.topLevelCounts["sub"]; got != 1 {
			t.Errorf("top-level test count for sub = %d, want 1", got)
		}
		if got := p.subTestCounts["sub"]; got != 1 {
			t.Errorf("subtest count for sub = %d, want 1", got)
		}
		if len(p.results) != 1 {
			t.Fatalf("got %d package results, want 1: %+v", len(p.results), p.results)
		}
		r := p.results[0]
		if r.Name != "sub" || r.Status != "ok" {
			t.Errorf("result = %+v, want name=sub status=ok", r)
		}
		if r.Coverage != 75.0 {
			t.Errorf("coverage = %v, want 75.0", r.Coverage)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("parseTestOutput hung on a non-JSON line")
	}
}

// Lines that do not begin with "{" (blanks, plain log text) must be ignored
// while a final line lacking a trailing newline is still parsed.
func TestParseTestOutputParsesFinalLineWithoutNewline(t *testing.T) {
	cfg := Config{ModulePrefix: "covdemo/"}

	// No trailing newline on the last event.
	input := `{"Action":"run","Package":"covdemo/sub","Test":"TestA"}
` + "\n" + // blank line
		`{"Action":"output","Package":"covdemo/sub","Output":"ok  \tcovdemo/sub\t0.010s\tcoverage: 50.0% of statements\n"}`

	var results []PackageResult
	captureStdout(t, func() {
		results, _, _ = parseTestOutput(cfg, strings.NewReader(input))
	})

	if len(results) != 1 {
		t.Fatalf("got %d package results, want 1: %+v", len(results), results)
	}
	if results[0].Coverage != 50.0 {
		t.Errorf("coverage = %v, want 50.0", results[0].Coverage)
	}
}
