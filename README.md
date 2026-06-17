# go-cov

Reusable Go coverage runner and reporter extracted from Haystack and Syntrix.

It runs `go test -json` with coverage, prints package/function summaries, generates a local HTML report, and ranks uncovered blocks with a lightweight AST analysis.

The repo is CLI-first. Implementation packages live under `internal/` so consumers depend on the command contract instead of an unstable Go API.

## Layout

```text
cmd/go-cov/          CLI entrypoint
internal/coverage/   runner, parsers, reports, and AST analysis
```

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

The module prefix is detected with `go list -m` and stripped from displayed paths. In a Go workspace (`go.work`) every main module's prefix is detected and the longest match is stripped, so packages from all workspace modules are shortened correctly. Override detection with `--module-prefix` when needed (an explicit prefix is used verbatim and disables auto-detection).

`go-cov` intentionally does not expose `go test -coverpkg`. Coverage should use Go's default package coverage universe for the selected packages, with policy exceptions expressed as explicit excludes.

Excludes are applied uniformly across every dimension. When a file or function is excluded (`exclude.files`, `exclude.funcs`, `exclude.func_suffixes`), its statements are removed from the **total** and **per-package** percentages as well as from the function list and the uncovered-block report — so excluding `foo.go` makes it disappear from the gate entirely, not just from the per-function output. Likewise, `exclude.packages` packages are never built or measured, so they never appear anywhere.

## Configuration

By default, `go-cov` reads `.go-cov.toml` from the current working directory. Use `--config path/to/file.toml` to load a different file.

Configuration precedence is:

```text
defaults < config file < environment variables < flags
```

Example:

```toml
[thresholds]
total = 85
function = 50
package = 70
print = 85

[test]
timeout = "15m"
race = false
tags = ["sqlite_fts5", "race_heavy"]

[exclude]
packages = [
  "borgee-server/scripts/",
  "borgee-server/cmd",
  "borgee-server/internal/testutil",
  "borgee-server/internal/api/cm5stance",
  "borgee-server/internal/testutil/regression_suite",
]
files = ["internal/testutil/", "main.go"]
funcs = []
func_suffixes = ["ForTest"]

[html]
enabled = false
path = ".coverage/test_coverage.html"

[critical_blocks]
fail = false
```

Supported config keys:

- `thresholds.function`
- `thresholds.package`
- `thresholds.print`
- `thresholds.total`
- `test.timeout`
- `test.race`
- `test.tags`
- `exclude.packages`
- `exclude.files`
- `exclude.funcs`
- `exclude.func_suffixes`
- `html.enabled`
- `html.path`
- `critical_blocks.fail`

Optional metadata keys:

- `project`
- `module_prefix`

`exclude.func_suffixes` defaults to `["ForTest"]` (test helpers that live in non-test files). Set it to `[]` to disable, or override it with your own suffixes.

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
- `EXCLUDE_FUNC_SUFFIXES`
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
