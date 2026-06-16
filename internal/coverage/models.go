package coverage

import "fmt"

// Block represents a single coverage block from the coverage file
type Block struct {
	File      string
	StartLine int
	StartCol  int
	EndLine   int
	EndCol    int
	Count     int
}

// MergedBlock represents a merged uncovered code block
type MergedBlock struct {
	File           string
	StartLine      int
	StartCol       int
	EndLine        int
	EndCol         int
	NumLines       int
	EffectiveLines int
	Level          string
	FixAction      string
	NoCov          bool // true if any line in the block contains // nocov
}

// ShouldPrint returns true if the block should be printed (non-LOW level)
func (b *MergedBlock) ShouldPrint() bool {
	return b.Level != "LOW"
}

// Print outputs the block information with formatting.
// ANSI color is applied only when colorEnabled is true; otherwise the level and
// fix-action columns are emitted as plain text so redirected output stays clean.
// In CI mode a CRITICAL block is prefixed with a GitHub Actions error annotation
// pinned to its file/line.
func (b *MergedBlock) Print(locWidth int, colorEnabled, ciMode bool) {
	if b.ShouldPrint() {
		rangeStr := fmt.Sprintf("%s:(%d:%d)-(%d:%d)", b.File, b.StartLine, b.StartCol, b.EndLine, b.EndCol)
		linesStr := fmt.Sprintf("%d", b.NumLines)

		level := b.Level
		fixAction := b.FixAction
		switch b.Level {
		case "CRITICAL":
			level = colorize(level, ColorRed, colorEnabled)
			fixAction = colorize(fixAction, ColorRed, colorEnabled)
		case "HIGH":
			level = colorize(level, ColorYellow, colorEnabled)
		case "MEDIUM":
			level = colorize(level, ColorBlue, colorEnabled)
		case "LOW":
			level = colorize(level, ColorGreen, colorEnabled)
		}
		if ciMode && b.Level == "CRITICAL" {
			fmt.Printf("::error file=%s,line=%d::", b.File, b.StartLine)
		}
		fmt.Printf("%-*s %-6s %-6d %-10s %s\n", locWidth, rangeStr, linesStr, b.EffectiveLines, level, fixAction)
	}
}
