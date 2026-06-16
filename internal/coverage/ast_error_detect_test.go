package coverage

import (
	"go/parser"
	"testing"
)

func TestIsErrorName(t *testing.T) {
	t.Parallel()

	positive := []string{"err", "Err", "error", "Error", "errs", "errors",
		"myErr", "parseError", "errCh", "readErr", "numErrors", "err_msg", "HTTPErr", "errVal"}
	for _, n := range positive {
		if !isErrorName(n) {
			t.Errorf("isErrorName(%q) = false, want true", n)
		}
	}

	// Substrings that contain "err" but are not the word err/error.
	negative := []string{"errand", "terror", "iterator", "number", "ferry", "merry", "", "e", "Errand"}
	for _, n := range negative {
		if isErrorName(n) {
			t.Errorf("isErrorName(%q) = true, want false", n)
		}
	}
}

func TestIsErrorCheckWordAccurate(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		"err != nil":     true,
		"nil != err":     true,
		"readErr == nil": true,
		"errand != nil":  false, // substring "err" but not the word
		"n != nil":       false,
		"ok":             false, // not a binary expr
	}
	for src, want := range cases {
		e, perr := parser.ParseExpr(src)
		if perr != nil {
			t.Fatalf("ParseExpr(%q): %v", src, perr)
		}
		if got := isErrorCheck(e); got != want {
			t.Errorf("isErrorCheck(%q) = %v, want %v", src, got, want)
		}
	}
}

func TestIsErrorExprWordAccurate(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		"err":              true,
		"parseError":       true,
		`fmt.Errorf("x")`:  true,
		"errors.New(s)":    true,
		"errors.Wrap(e,s)": true,
		"errand":           false, // substring only
		"widget.New()":     false, // New on a non-errors package must not match
		"compute()":        false,
		"x + y":            false,
	}
	for src, want := range cases {
		e, perr := parser.ParseExpr(src)
		if perr != nil {
			t.Fatalf("ParseExpr(%q): %v", src, perr)
		}
		if got := isErrorExpr(e); got != want {
			t.Errorf("isErrorExpr(%q) = %v, want %v", src, got, want)
		}
	}
}
