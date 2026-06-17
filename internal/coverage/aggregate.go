package coverage

import (
	"bufio"
	"fmt"
	"go/ast"
	"os"
	"strings"
)

// covAgg accumulates covered vs. total executable statements for a coverage
// dimension (the whole module, a single package, etc.). Coverage is weighted by
// statement count so it matches how `go tool cover` computes its percentages.
type covAgg struct {
	covered int
	total   int
}

// percent returns the coverage percentage and whether it is known. An aggregate
// with zero statements is "no data" (known=false), not a genuine 0%: this keeps
// an empty/fully-excluded dimension from being reported or gated as 0% covered.
func (a covAgg) percent() (float64, bool) {
	if a.total == 0 {
		return 0, false
	}
	return float64(a.covered) / float64(a.total) * 100, true
}

// aggregateCoverage sums statement counts across blocks to produce the overall
// total and a per-package breakdown keyed by each file's package directory.
// Because the caller passes only the blocks that survived exclusion, excluded
// files and functions are absent from every returned aggregate.
func aggregateCoverage(blocks []Block) (covAgg, map[string]covAgg) {
	var total covAgg
	perPackage := make(map[string]covAgg)
	for _, b := range blocks {
		pkg := packageOf(b.File)
		agg := perPackage[pkg]

		total.total += b.NumStmt
		agg.total += b.NumStmt
		if b.Count > 0 {
			total.covered += b.NumStmt
			agg.covered += b.NumStmt
		}

		perPackage[pkg] = agg
	}
	return total, perPackage
}

// packageOf returns the package import path of a coverage block's file, i.e. the
// directory portion of the (module-prefix-stripped) file path. Coverage profiles
// always use forward slashes, matching the stripped package names parsed from
// `go test` output, so the keys line up with PackageResult.Name.
func packageOf(file string) string {
	if i := strings.LastIndexByte(file, '/'); i >= 0 {
		return file[:i]
	}
	return ""
}

// applyComputedPackageCoverage overwrites each package result's coverage with the
// recomputed, exclusion-aware value so the package summary agrees with the total
// and function dimensions. Packages without computed data (e.g. no profile entry
// after exclusion) keep whatever `go test` reported.
func applyComputedPackageCoverage(results []PackageResult, perPackage map[string]covAgg, cfg Config) {
	for i := range results {
		agg, ok := perPackage[rootPackageKey(cfg, results[i].Name)]
		if !ok {
			continue
		}
		if pct, known := agg.percent(); known {
			results[i].Coverage = pct
			results[i].CoverageStr = fmt.Sprintf("%.1f%%", pct)
		}
	}
}

// rootPackageKey maps a package result name to the key aggregateCoverage uses.
// They agree for every package except a module's root package: `go test` reports
// it under the full import path, which stripModulePrefix leaves unstripped (the
// path lacks the trailing slash the prefix is matched with), whereas the package's
// root files key under "" via packageOf. Normalize that one case to "".
//
// Only done for a single module. In a Go workspace several modules' root packages
// would all collapse to "" and collide, so those are left to a harmless lookup
// miss that preserves the go-test-reported number rather than a wrong merge.
func rootPackageKey(cfg Config, name string) string {
	prefixes := cfg.ModulePrefixes
	if len(prefixes) == 0 && cfg.ModulePrefix != "" {
		prefixes = []string{cfg.ModulePrefix}
	}
	if len(prefixes) != 1 {
		return name
	}
	if name != "" && name+"/" == prefixes[0] {
		return ""
	}
	return name
}

// loadCoverageBlocks reads the coverage profile and returns the blocks that
// survive both file and function exclusion, each carrying its statement count.
// The returned blocks are the single source of truth for every coverage
// dimension (total, per-package, and the uncovered-block report), so an excluded
// file or function is removed from all of them consistently.
func loadCoverageBlocks(cfg Config, astCache *ASTCache) ([]Block, error) {
	file, err := os.Open(cfg.CoverProfile)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	excluder := newFuncExcluder(cfg, astCache)
	var blocks []Block
	// Merge duplicate block keys the way `go tool cover` does: a block that
	// appears more than once (atomic-mode merges can emit it per test binary)
	// must be counted once, with its execution counts summed. index maps a
	// block's location to its slot in blocks so order is preserved.
	index := make(map[blockKey]int)
	add := func(b Block) {
		k := blockKey{b.File, b.StartLine, b.StartCol, b.EndLine, b.EndCol}
		if i, ok := index[k]; ok {
			blocks[i].Count += b.Count
			return
		}
		index[k] = len(blocks)
		blocks = append(blocks, b)
	}

	scanner := bufio.NewScanner(file)

	// The first line is the "mode:" header; if it is missing, treat the line as a
	// block (mirrors the tolerant parsing used elsewhere).
	if scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "mode:") {
			if b, ok := parseLine(line, cfg); ok && !excluder.excludes(b) {
				add(b)
			}
		}
	}
	for scanner.Scan() {
		if b, ok := parseLine(scanner.Text(), cfg); ok && !excluder.excludes(b) {
			add(b)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return blocks, nil
}

// blockKey identifies a coverage block by its file and exact source range so
// duplicate profile entries for the same block can be merged.
type blockKey struct {
	file     string
	startLn  int
	startCol int
	endLn    int
	endCol   int
}

// funcExcluder decides whether a coverage block belongs to a function that the
// configuration excludes by name. It attributes a block to a function via the
// source AST (the block's start line falling inside the function's line span)
// and caches each file's excluded spans so every file is parsed at most once.
type funcExcluder struct {
	cfg      Config
	astCache *ASTCache
	spans    map[string][]lineSpan
}

// lineSpan is an inclusive 1-based line range [start, end].
type lineSpan struct {
	start int
	end   int
}

func newFuncExcluder(cfg Config, astCache *ASTCache) *funcExcluder {
	return &funcExcluder{cfg: cfg, astCache: astCache, spans: make(map[string][]lineSpan)}
}

// excludes reports whether the block falls inside an excluded function. When no
// function excludes are configured this is a no-op, so no AST is parsed.
func (fe *funcExcluder) excludes(b Block) bool {
	if len(fe.cfg.ExcludeFuncs) == 0 && len(fe.cfg.ExcludeFuncSuffixes) == 0 {
		return false
	}
	for _, s := range fe.spansFor(b.File) {
		if b.StartLine >= s.start && b.StartLine <= s.end {
			return true
		}
	}
	return false
}

// spansFor returns the line spans of the excluded functions declared in file.
// A file that cannot be parsed yields no spans, so its blocks are never
// function-excluded — exclusion never over-reaches on unparseable sources.
func (fe *funcExcluder) spansFor(file string) []lineSpan {
	if spans, ok := fe.spans[file]; ok {
		return spans
	}

	var spans []lineSpan
	if fileAST, fset, err := fe.astCache.GetAST(file); err == nil {
		for _, decl := range fileAST.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name == nil {
				continue
			}
			if fe.nameExcluded(fn.Name.Name) {
				spans = append(spans, lineSpan{
					start: fset.Position(fn.Pos()).Line,
					end:   fset.Position(fn.End()).Line,
				})
			}
		}
	}

	fe.spans[file] = spans
	return spans
}

// nameExcluded matches a function name against the configured excludes using the
// same rules as the function-list report: substring for ExcludeFuncs and suffix
// for ExcludeFuncSuffixes.
func (fe *funcExcluder) nameExcluded(name string) bool {
	for _, ef := range fe.cfg.ExcludeFuncs {
		if ef != "" && strings.Contains(name, ef) {
			return true
		}
	}
	for _, suffix := range fe.cfg.ExcludeFuncSuffixes {
		if suffix != "" && strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}
