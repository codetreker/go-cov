# go-cov

Reusable Go coverage runner and reporter extracted from Haystack and Syntrix.

It runs `go test -json` with coverage, prints package/function summaries, generates a local HTML report, and ranks uncovered blocks with a lightweight AST analysis.

## Usage

Run from the root of a Go module:

```bash
go run github.com/codetreker/go-cov/cmd/go-cov@latest
```

Common options:

```bash
go run github.com/codetreker/go-cov/cmd/go-cov@latest \
  --exclude-packages "cmd/,scripts/,pkg/benchmark/" \
  --skip-result-packages "tests/" \
  --exclude-files "internal/testutil/,main.go" \
  --exclude-funcs "Replay,Close" \
  --tags "sqlite_fts5 race_heavy" \
  --html-out ".myproject/test_coverage.html"
```

The module prefix is detected with `go list -m` and stripped from displayed paths. Override it with `--module-prefix` when needed.

`go-cov` intentionally does not expose `go test -coverpkg`. Coverage should use Go's default package coverage universe for the selected packages, with policy exceptions expressed as explicit excludes.

## Environment Compatibility

The CLI also reads the existing environment variables used by the in-repo scripts:

- `CI`
- `COVERPROFILE`
- `THRESHOLD_FUNC`
- `THRESHOLD_PACKAGE`
- `THRESHOLD_PRINT`
- `THRESHOLD_TOTAL`
- `UNCOVERED_LIMIT`
- `EXCLUDE_PACKAGES`
- `SKIP_RESULT_PACKAGES`
- `EXCLUDE_FILES`
- `EXCLUDE_FUNCS`
- `MODULE_PREFIX`
- `PROJECT_NAME`
- `HTML_OUT`
- `TEST_TIMEOUT`
- `BUILD_TAGS`
- `GENERATE_HTML`
- `RACE_DETECTION`
- `FAIL_ON_CRITICAL_BLOCKS`

Flags override environment values.

## Migration

Replace project-local wrappers with a pinned version:

```bash
go run github.com/codetreker/go-cov/cmd/go-cov@v0.1.0 "$@"
```

For CI, keep project-specific exclusions in the workflow or wrapper script instead of forking the tool.
