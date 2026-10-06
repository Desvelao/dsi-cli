package cli

import (
	"fmt"
	"strings"

	"github.com/Desvelao/dsipy/internal/pyutil"
	"github.com/pmezard/go-difflib/difflib"
)

// showDiff prints a unified diff (or a "No differences." note).
func (e *Env) showDiff(oldText, newText, fromFile, toFile string) {
	lines := func(s string) []string {
		parts := pyutil.SplitLines(s)
		for i := range parts {
			parts[i] += "\n"
		}
		return parts
	}
	diff, _ := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A: lines(oldText), B: lines(newText), FromFile: fromFile, ToFile: toFile, Context: 3, Eol: "\n",
	})
	diff = strings.TrimRight(diff, "\n")
	if strings.TrimSpace(diff) == "" {
		fmt.Fprintln(e.Out, e.paint(green, "  No differences."))
		return
	}
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
			fmt.Fprintln(e.Out, e.paint(bold, line))
		case strings.HasPrefix(line, "+"):
			fmt.Fprintln(e.Out, e.paint(green, line))
		case strings.HasPrefix(line, "-"):
			fmt.Fprintln(e.Out, e.paint(red, line))
		case strings.HasPrefix(line, "@@"):
			fmt.Fprintln(e.Out, e.paint(cyan, line))
		default:
			fmt.Fprintln(e.Out, line)
		}
	}
}
