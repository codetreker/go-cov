package main

import (
	"fmt"
	"os"

	coverage "github.com/codetreker/go-cov"
)

func main() {
	cfg, err := coverage.ConfigFromEnv(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	os.Exit(coverage.Run(cfg))
}
