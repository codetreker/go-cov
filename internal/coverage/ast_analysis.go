package coverage

import (
	"go/ast"
	"go/token"
	"strings"
	"unicode"
)

const CRITICAL_EFFECTIVE_LINES = 3

// isNodeInRange checks if an AST node is within the specified range (considering column info)
// A node is considered "in range" if its starting position is within the block range.
// This ensures we only count nodes that actually start within the uncovered block.
func isNodeInRange(fset *token.FileSet, n ast.Node, startLine, startCol, endLine, endCol int) bool {
	pos := fset.Position(n.Pos())
	end := fset.Position(n.End())

	// Node ends before block starts
	if end.Line < startLine {
		return false
	}
	if end.Line == startLine && end.Column < startCol {
		return false
	}

	// Node starts after block ends
	if pos.Line > endLine {
		return false
	}
	if pos.Line == endLine && pos.Column > endCol {
		return false
	}

	// For better precision: node must START within the block
	// This avoids counting parent nodes (like IfStmt) when only their body is uncovered
	if pos.Line < startLine {
		return false
	}
	if pos.Line == startLine && pos.Column < startCol {
		return false
	}

	return true
}

// BlockAnalysis holds the AST analysis results for a code block
type BlockAnalysis struct {
	HasExportedFunc bool // Public API - high priority
	HasExportedType bool // Exported type methods
	HasConcurrency  bool // goroutine, channel, select
	HasErrorHandle  bool // error checking/returning
	FuncCallCount   int  // function calls
	BranchCount     int  // if, switch, for, range (cyclomatic complexity)
	FuncCount       int  // number of functions
}

// Score calculates a priority score based on analysis
func (a *BlockAnalysis) Score() int {
	score := 0
	if a.HasExportedFunc {
		score += 30
	}
	if a.HasExportedType {
		score += 20
	}
	if a.HasConcurrency {
		score += 20
	}
	if a.HasErrorHandle {
		score += 10
	}
	score += a.BranchCount * 15
	score += a.FuncCallCount * 5
	score += a.FuncCount * 3
	return score
}

// AnalyzeBlockWithAST uses AST to analyze a code block and determine its priority level
func AnalyzeBlockWithAST(b *MergedBlock, astCache *ASTCache, fileCache *FileCache) {
	// First, calculate effective lines (non-empty, non-comment lines)
	// Consider column information for accurate counting
	lines, err := fileCache.GetLines(b.File)
	if err != nil {
		b.Level = "UNKNOWN"
		return
	}

	effectiveCount := 0
	for i := b.StartLine; i <= b.EndLine; i++ {
		if i < 1 || i > len(lines) {
			continue
		}

		fullLine := lines[i-1]
		var segment string

		if i == b.StartLine && i == b.EndLine {
			// Same line: only take content between StartCol and EndCol
			startIdx := min(b.StartCol-1, len(fullLine))
			endIdx := min(b.EndCol-1, len(fullLine))
			if startIdx < endIdx && startIdx >= 0 {
				segment = fullLine[startIdx:endIdx]
			}
		} else if i == b.StartLine {
			// Start line: from StartCol to end of line
			startIdx := min(b.StartCol-1, len(fullLine))
			if startIdx >= 0 && startIdx < len(fullLine) {
				segment = fullLine[startIdx:]
			}
		} else if i == b.EndLine {
			// End line: from beginning to EndCol
			endIdx := min(b.EndCol-1, len(fullLine))
			if endIdx > 0 {
				segment = fullLine[:endIdx]
			}
		} else {
			// Middle lines: entire line
			segment = fullLine
		}

		if !isLineIgnorable(strings.TrimSpace(segment)) {
			effectiveCount++
		}
	}
	b.EffectiveLines = effectiveCount

	// Check for // nocov annotation in block lines
	for i := b.StartLine; i <= b.EndLine; i++ {
		if i >= 1 && i <= len(lines) {
			if strings.Contains(lines[i-1], "// nocov") {
				b.NoCov = true
				b.Level = "EXCLUDED"
				b.FixAction = "nocov"
				return
			}
		}
	}

	// Parse AST and analyze
	fileAST, fset, err := astCache.GetAST(b.File)
	if err != nil {
		// Fallback to simple analysis if AST parsing fails
		b.Level = classifyByEffectiveLines(effectiveCount)
		b.FixAction = getFixAction(b.Level)
		return
	}

	analysis := analyzeASTInRange(fileAST, fset, b.StartLine, b.StartCol, b.EndLine, b.EndCol)
	score := analysis.Score()

	// Determine level based on score
	switch {
	case score >= 30 || (analysis.HasExportedFunc && effectiveCount >= CRITICAL_EFFECTIVE_LINES):
		b.Level = "CRITICAL"
		b.FixAction = "Required"
	case score >= 25 || analysis.HasConcurrency:
		b.Level = "HIGH"
		b.FixAction = "Suggested"
	case score >= 10 || analysis.HasErrorHandle:
		b.Level = "MEDIUM"
		b.FixAction = "Consider"
	default:
		b.Level = "LOW"
		b.FixAction = ""
	}
}

// analyzeASTInRange walks the AST and collects information about nodes in the given line range
// It considers column information for precise node filtering
func analyzeASTInRange(file *ast.File, fset *token.FileSet, startLine, startCol, endLine, endCol int) BlockAnalysis {
	var analysis BlockAnalysis

	ast.Inspect(file, func(n ast.Node) bool {
		if n == nil {
			return true
		}

		// Check if node is within our range (considering column information)
		if !isNodeInRange(fset, n, startLine, startCol, endLine, endCol) {
			return true
		}

		switch v := n.(type) {
		case *ast.FuncDecl:
			analysis.FuncCount++
			if v.Name != nil && v.Name.IsExported() {
				analysis.HasExportedFunc = true
			}
			// Check if it's a method on an exported type
			if v.Recv != nil && len(v.Recv.List) > 0 {
				if isExportedReceiver(v.Recv.List[0].Type) {
					analysis.HasExportedType = true
				}
			}

		case *ast.IfStmt:
			analysis.BranchCount++
			// Check for error handling pattern: if err != nil
			if isErrorCheck(v.Cond) {
				analysis.HasErrorHandle = true
			}

		case *ast.ForStmt, *ast.RangeStmt:
			analysis.BranchCount++

		case *ast.SwitchStmt, *ast.TypeSwitchStmt:
			analysis.BranchCount++

		case *ast.SelectStmt:
			analysis.BranchCount++
			analysis.HasConcurrency = true

		case *ast.GoStmt:
			analysis.HasConcurrency = true

		case *ast.SendStmt: // channel send: ch <- value
			analysis.HasConcurrency = true

		case *ast.ChanType: // channel type declaration
			analysis.HasConcurrency = true

		case *ast.ReturnStmt:
			// Check if returning an error
			for _, result := range v.Results {
				if isErrorExpr(result) {
					analysis.HasErrorHandle = true
				}
			}

		case *ast.CaseClause:
			analysis.BranchCount++
		case *ast.CallExpr:
			// Check for error handling calls
			if !isErrorExpr(v.Fun) {
				analysis.FuncCallCount++
			}
		}

		return true
	})

	return analysis
}

// isExportedReceiver checks if the receiver type is exported
func isExportedReceiver(expr ast.Expr) bool {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.IsExported()
	case *ast.StarExpr: // *Type
		return isExportedReceiver(t.X)
	}
	return false
}

// splitIdentWords splits a Go identifier into lowercased words on camelCase,
// acronym, underscore, and digit boundaries (e.g. "parseErrCh" -> ["parse",
// "err", "ch"], "HTTPErr" -> ["http", "err"]). Used for word-accurate matching
// so substrings like "errand"/"terror" are not mistaken for the word "err".
func splitIdentWords(name string) []string {
	var words []string
	var cur []rune
	rs := []rune(name)
	push := func() {
		if len(cur) > 0 {
			words = append(words, strings.ToLower(string(cur)))
			cur = cur[:0]
		}
	}
	for i, r := range rs {
		if r == '_' {
			push()
			continue
		}
		if i > 0 {
			prev := rs[i-1]
			lowerToUpper := unicode.IsUpper(r) && (unicode.IsLower(prev) || unicode.IsDigit(prev))
			acronymEnd := unicode.IsUpper(prev) && unicode.IsUpper(r) && i+1 < len(rs) && unicode.IsLower(rs[i+1])
			if lowerToUpper || acronymEnd {
				push()
			}
		}
		cur = append(cur, r)
	}
	push()
	return words
}

// isErrorName reports whether an identifier is an error-typed name by Go
// convention — i.e. one of its camelCase/underscore words is err/errs/error/
// errors. This matches err, myErr, parseError, errCh while rejecting errand,
// terror, iterator.
func isErrorName(name string) bool {
	for _, w := range splitIdentWords(name) {
		switch w {
		case "err", "errs", "error", "errors":
			return true
		}
	}
	return false
}

// isErrorConstructorCall reports whether a call expression constructs/wraps an
// error (fmt.Errorf, errors.New/Wrap/Wrapf, xerrors.Errorf, ...). New/Wrap/Wrapf
// are only treated as error constructors when the package qualifier is itself an
// errors-ish identifier, so an unrelated pkg.New() is not misread as an error.
func isErrorConstructorCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	switch sel.Sel.Name {
	case "Errorf":
		return true
	case "New", "Wrap", "Wrapf":
		if pkg, ok := sel.X.(*ast.Ident); ok {
			return isErrorName(pkg.Name)
		}
	}
	return false
}

// isErrorCheck checks if the condition is an error check (err != nil or err == nil)
func isErrorCheck(expr ast.Expr) bool {
	binExpr, ok := expr.(*ast.BinaryExpr)
	if !ok {
		return false
	}
	if ident, ok := binExpr.X.(*ast.Ident); ok && isErrorName(ident.Name) {
		return true
	}
	if ident, ok := binExpr.Y.(*ast.Ident); ok && isErrorName(ident.Name) {
		return true
	}
	return false
}

// isErrorExpr checks if an expression is likely an error (named like an error or
// constructed by an error constructor such as fmt.Errorf / errors.New).
func isErrorExpr(expr ast.Expr) bool {
	if ident, ok := expr.(*ast.Ident); ok {
		return isErrorName(ident.Name)
	}
	if call, ok := expr.(*ast.CallExpr); ok {
		return isErrorConstructorCall(call)
	}
	return false
}

// classifyByEffectiveLines is a fallback when AST parsing fails
func classifyByEffectiveLines(effectiveLines int) string {
	switch {
	case effectiveLines >= CRITICAL_EFFECTIVE_LINES:
		return "HIGH"
	case effectiveLines >= 2:
		return "MEDIUM"
	default:
		return "LOW"
	}
}

// getFixAction returns the fix action string for a given level
func getFixAction(level string) string {
	switch level {
	case "CRITICAL":
		return "Required"
	case "HIGH":
		return "Suggested"
	case "MEDIUM":
		return "Consider"
	default:
		return ""
	}
}

// isLineIgnorable checks if a line should be ignored in coverage analysis
func isLineIgnorable(line string) bool {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "//") || line == "}" || line == "){" || line == ")" || line == "]" || line == "}," {
		return true
	}
	return false
}
