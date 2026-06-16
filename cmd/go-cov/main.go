package main

import (
	"fmt"
	"os"

	"github.com/codetreker/go-cov/internal/coverage"
)

func main() {
	cfg, err := coverage.ConfigFromEnv(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	os.Exit(coverage.Run(os.Stdout, cfg))
}
