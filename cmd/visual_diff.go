package cmd

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

// WordChange represents a single word-level change between two strings.
type WordChange struct {
	Type string // "equal", "insert", "delete"
	Text string
}

// FileDiffStat holds statistics about a single file's diff.
type FileDiffStat struct {
	Path      string
	Additions int
	Deletions int
	Status    string // "M", "A", "D", "R"
}

// DiffTheme defines ANSI color codes for rendering diffs.
type DiffTheme struct {
	Added   string
	Removed string
	Changed string
	Context string
	LineNo  string
	Header  string
	WordAdd string
	WordDel string
	Reset   string
}

// DefaultDiffTheme returns a DiffTheme using rho's semantic palette so diff
// output reads consistently with the rest of the UI: additions in doneGreen,
// deletions in errorCoral, hunk markers in infoSky, file headers in
// fileHeaderBlue — each color mapped to one meaning, never reused for another.
func DefaultDiffTheme() DiffTheme {
	return DiffTheme{
		Added:   ansiDone,
		Removed: ansiCoral,
		Changed: ansiSky,
		Context: ansiGrayDim,
		LineNo:  ansiGrayDim,
		Header:  ansiFileBlue + ansiBold,
		WordAdd: ansiDone + ansiBold,
		WordDel: ansiCoral + ansiBold,
		Reset:   ansiReset,
	}
}

// VisualDiff renders diffs with colors and formatting for TUI display.
type VisualDiff struct {
	Width           int
	Theme           DiffTheme
	ShowLineNumbers bool
	ContextLines    int
	WordLevel       bool
}

// NewVisualDiff creates a VisualDiff renderer with the given terminal width.
func NewVisualDiff(width int) *VisualDiff {
	if width < 40 {
		width = 80
	}
	return &VisualDiff{
		Width:           width,
		Theme:           DefaultDiffTheme(),
		ShowLineNumbers: true,
		ContextLines:    3,
		WordLevel:       true,
	}
}

// vdDiffLine is one parsed line of a unified diff, tagged by its role.
type vdDiffLine struct {
	text     string
	lineType byte // '+', '-', ' ', '@', 'd'
}

// isIsolatedReplacePair reports whether parsed[i] is a lone '-' immediately
// followed by a lone '+' — a genuine single-line replacement. Word-level
// diffing is only meaningful for that case; inside a multi-line add/remove
// block, naively pairing each '-' with the next '+' pairs unrelated lines
// (e.g. the last removed line with the first added line) and produces
// nonsensical highlighting, so those blocks render as plain colored lines.
func isIsolatedReplacePair(parsed []vdDiffLine, i int) bool {
	if i+1 >= len(parsed) || parsed[i].lineType != '-' || parsed[i+1].lineType != '+' {
		return false
	}
	if i > 0 && parsed[i-1].lineType == '-' {
		return false
	}
	if i+2 < len(parsed) && parsed[i+2].lineType == '+' {
		return false
	}
	return true
}

// looksLikeGitDiff reports whether content is a raw `git diff`-style patch
// (as opposed to plain command output that happens to contain a "-").
func looksLikeGitDiff(content string) bool {
	return strings.Contains(content, "diff --git ") ||
		strings.Contains(content, "\n@@ ") || strings.HasPrefix(content, "@@ ")
}

// renderGitDiffOutput renders a full `git diff` transcript (potentially many
// files) with per-file headers plus colorized, line-numbered hunks. This is
// what turns a wall of flat gray patch text into something scannable: file
// paths stand out in fileHeaderBlue, additions/deletions in doneGreen/errorCoral,
// hunk markers in infoSky, and metadata (index/mode lines) stays dim.
func renderGitDiffOutput(content string, width int) string {
	vd := NewVisualDiff(width)
	fileHeaderStyle := ansiFileBlue + ansiBold
	metaStyle := ansiGrayDim

	lines := strings.Split(content, "\n")
	var out strings.Builder
	var hunk []string

	flushHunk := func() {
		if len(hunk) == 0 {
			return
		}
		rendered := strings.TrimRight(vd.RenderInline(strings.Join(hunk, "\n")), "\n")
		if rendered != "" {
			out.WriteString(rendered)
			out.WriteByte('\n')
		}
		hunk = hunk[:0]
	}

	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			flushHunk()
			label := strings.TrimPrefix(line, "diff --git ")
			if fields := strings.Fields(label); len(fields) == 2 {
				label = strings.TrimPrefix(fields[1], "b/")
			}
			out.WriteString(fileHeaderStyle + label + ansiReset + "\n")
		case strings.HasPrefix(line, "index "),
			strings.HasPrefix(line, "new file mode"),
			strings.HasPrefix(line, "deleted file mode"),
			strings.HasPrefix(line, "similarity index"),
			strings.HasPrefix(line, "rename from"),
			strings.HasPrefix(line, "rename to"):
			flushHunk()
			out.WriteString(metaStyle + line + ansiReset + "\n")
		default:
			hunk = append(hunk, line)
		}
	}
	flushHunk()
	return strings.TrimRight(out.String(), "\n")
}

// RenderInline renders a unified diff with colors and line numbers.
func (vd *VisualDiff) RenderInline(diff string) string {
	if diff == "" {
		return ""
	}

	lines := strings.Split(diff, "\n")
	var out strings.Builder

	oldLine := 0
	newLine := 0

	// Collect lines for word-level diffing
	var parsed []vdDiffLine

	for _, line := range lines {
		if len(line) == 0 {
			parsed = append(parsed, vdDiffLine{text: "", lineType: ' '})
			continue
		}
		switch {
		case strings.HasPrefix(line, "@@"):
			parsed = append(parsed, vdDiffLine{text: line, lineType: '@'})
		case strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++"):
			parsed = append(parsed, vdDiffLine{text: line, lineType: 'd'})
		case line[0] == '+':
			parsed = append(parsed, vdDiffLine{text: line[1:], lineType: '+'})
		case line[0] == '-':
			parsed = append(parsed, vdDiffLine{text: line[1:], lineType: '-'})
		default:
			parsed = append(parsed, vdDiffLine{text: line, lineType: ' '})
		}
	}

	i := 0
	for i < len(parsed) {
		dl := parsed[i]
		switch dl.lineType {
		case 'd':
			out.WriteString(vd.Theme.Header + dl.text + vd.Theme.Reset + "\n")
		case '@':
			out.WriteString(vd.Theme.Changed + dl.text + vd.Theme.Reset + "\n")
			// Parse hunk header for line numbers
			oldLine, newLine = parseHunkHeader(dl.text)
		case '-':
			// Check if next lines are '+' for word-level diff
			if vd.WordLevel && isIsolatedReplacePair(parsed, i) {
				oldRendered, newRendered := vd.RenderWordDiff(dl.text, parsed[i+1].text)
				if vd.ShowLineNumbers {
					out.WriteString(fmt.Sprintf("%s%4d%s %s-%s%s\n",
						vd.Theme.LineNo, oldLine, vd.Theme.Reset,
						vd.Theme.Removed, oldRendered, vd.Theme.Reset))
					newLine++
					oldLine++
					i++
					out.WriteString(fmt.Sprintf("%s%4d%s %s+%s%s\n",
						vd.Theme.LineNo, newLine-1, vd.Theme.Reset,
						vd.Theme.Added, newRendered, vd.Theme.Reset))
				} else {
					out.WriteString(fmt.Sprintf("%s-%s%s\n", vd.Theme.Removed, oldRendered, vd.Theme.Reset))
					oldLine++
					i++
					out.WriteString(fmt.Sprintf("%s+%s%s\n", vd.Theme.Added, newRendered, vd.Theme.Reset))
					newLine++
				}
			} else {
				if vd.ShowLineNumbers {
					out.WriteString(fmt.Sprintf("%s%4d%s %s-%s%s\n",
						vd.Theme.LineNo, oldLine, vd.Theme.Reset,
						vd.Theme.Removed, dl.text, vd.Theme.Reset))
				} else {
					out.WriteString(fmt.Sprintf("%s-%s%s\n", vd.Theme.Removed, dl.text, vd.Theme.Reset))
				}
				oldLine++
			}
		case '+':
			if vd.ShowLineNumbers {
				out.WriteString(fmt.Sprintf("%s%4d%s %s+%s%s\n",
					vd.Theme.LineNo, newLine, vd.Theme.Reset,
					vd.Theme.Added, dl.text, vd.Theme.Reset))
			} else {
				out.WriteString(fmt.Sprintf("%s+%s%s\n", vd.Theme.Added, dl.text, vd.Theme.Reset))
			}
			newLine++
		default:
			if vd.ShowLineNumbers {
				out.WriteString(fmt.Sprintf("%s%4d%s  %s\n",
					vd.Theme.LineNo, newLine, vd.Theme.Reset, dl.text))
			} else {
				out.WriteString(fmt.Sprintf(" %s\n", dl.text))
			}
			oldLine++
			newLine++
		}
		i++
	}

	return out.String()
}

// RenderSideBySide renders a diff in two-column format.
func (vd *VisualDiff) RenderSideBySide(diff string) string {
	if diff == "" {
		return ""
	}

	colWidth := (vd.Width - 3) / 2 // 3 for separator " | "
	if colWidth < 10 {
		colWidth = 38
	}
	numWidth := 4
	contentWidth := colWidth - numWidth - 1 // -1 for space after line number

	lines := strings.Split(diff, "\n")
	var out strings.Builder

	oldLine := 0
	newLine := 0

	var parsed []vdDiffLine

	for _, line := range lines {
		if len(line) == 0 {
			parsed = append(parsed, vdDiffLine{text: "", lineType: ' '})
			continue
		}
		switch {
		case strings.HasPrefix(line, "@@"):
			parsed = append(parsed, vdDiffLine{text: line, lineType: '@'})
		case strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++"):
			parsed = append(parsed, vdDiffLine{text: line, lineType: 'd'})
		case line[0] == '+':
			parsed = append(parsed, vdDiffLine{text: line[1:], lineType: '+'})
		case line[0] == '-':
			parsed = append(parsed, vdDiffLine{text: line[1:], lineType: '-'})
		default:
			parsed = append(parsed, vdDiffLine{text: line, lineType: ' '})
		}
	}

	// Render separator line
	separator := strings.Repeat("─", colWidth) + "┼" + strings.Repeat("─", colWidth)
	out.WriteString(separator + "\n")

	i := 0
	for i < len(parsed) {
		dl := parsed[i]
		switch dl.lineType {
		case 'd':
			// File headers span both columns
			header := vd.Theme.Header + dl.text + vd.Theme.Reset
			out.WriteString(header + "\n")
		case '@':
			hdr := vd.Theme.Changed + dl.text + vd.Theme.Reset
			out.WriteString(hdr + "\n")
			oldLine, newLine = parseHunkHeader(dl.text)
		case '-':
			// Check for paired addition
			if vd.WordLevel && isIsolatedReplacePair(parsed, i) {
				oldRendered, newRendered := vd.RenderWordDiff(dl.text, parsed[i+1].text)
				left := fmt.Sprintf("%s%4d%s %s%s%s",
					vd.Theme.LineNo, oldLine, vd.Theme.Reset,
					vd.Theme.Removed, padOrTruncate(oldRendered, contentWidth), vd.Theme.Reset)
				right := fmt.Sprintf("%s%4d%s %s%s%s",
					vd.Theme.LineNo, newLine, vd.Theme.Reset,
					vd.Theme.Added, padOrTruncate(newRendered, contentWidth), vd.Theme.Reset)
				out.WriteString(left + " │ " + right + "\n")
				oldLine++
				newLine++
				i++
			} else {
				left := fmt.Sprintf("%s%4d%s %s%s%s",
					vd.Theme.LineNo, oldLine, vd.Theme.Reset,
					vd.Theme.Removed, padOrTruncate(dl.text, contentWidth), vd.Theme.Reset)
				right := strings.Repeat(" ", colWidth)
				out.WriteString(left + " │ " + right + "\n")
				oldLine++
			}
		case '+':
			left := strings.Repeat(" ", colWidth)
			right := fmt.Sprintf("%s%4d%s %s%s%s",
				vd.Theme.LineNo, newLine, vd.Theme.Reset,
				vd.Theme.Added, padOrTruncate(dl.text, contentWidth), vd.Theme.Reset)
			out.WriteString(left + " │ " + right + "\n")
			newLine++
		default:
			left := fmt.Sprintf("%s%4d%s %s",
				vd.Theme.LineNo, oldLine, vd.Theme.Reset,
				padOrTruncate(dl.text, contentWidth))
			right := fmt.Sprintf("%s%4d%s %s",
				vd.Theme.LineNo, newLine, vd.Theme.Reset,
				padOrTruncate(dl.text, contentWidth))
			out.WriteString(left + " │ " + right + "\n")
			oldLine++
			newLine++
		}
		i++
	}

	return out.String()
}

// RenderWordDiff finds word-level changes between two lines and returns
// both lines with inline ANSI highlighting on the changed words.
func (vd *VisualDiff) RenderWordDiff(oldLine, newLine string) (string, string) {
	changes := FindWordChanges(oldLine, newLine)

	var oldOut, newOut strings.Builder

	for _, ch := range changes {
		switch ch.Type {
		case "equal":
			oldOut.WriteString(ch.Text)
			newOut.WriteString(ch.Text)
		case "delete":
			oldOut.WriteString(vd.Theme.WordDel + ch.Text + vd.Theme.Reset)
		case "insert":
			newOut.WriteString(vd.Theme.WordAdd + ch.Text + vd.Theme.Reset)
		}
	}

	return oldOut.String(), newOut.String()
}

// FindWordChanges computes word-level changes between old and new strings.
// It splits on whitespace boundaries and uses a longest common subsequence approach.
func FindWordChanges(old, new string) []WordChange {
	oldWords := splitWords(old)
	newWords := splitWords(new)

	// LCS-based diff on words
	lcs := computeLCS(oldWords, newWords)

	var changes []WordChange
	oi, ni, li := 0, 0, 0

	for li < len(lcs) {
		// Emit deletes until we reach the LCS word in old
		for oi < len(oldWords) && oldWords[oi] != lcs[li] {
			changes = append(changes, WordChange{Type: "delete", Text: oldWords[oi]})
			oi++
		}
		// Emit inserts until we reach the LCS word in new
		for ni < len(newWords) && newWords[ni] != lcs[li] {
			changes = append(changes, WordChange{Type: "insert", Text: newWords[ni]})
			ni++
		}
		// Emit the equal word
		changes = append(changes, WordChange{Type: "equal", Text: lcs[li]})
		oi++
		ni++
		li++
	}

	// Remaining words
	for oi < len(oldWords) {
		changes = append(changes, WordChange{Type: "delete", Text: oldWords[oi]})
		oi++
	}
	for ni < len(newWords) {
		changes = append(changes, WordChange{Type: "insert", Text: newWords[ni]})
		ni++
	}

	return changes
}

// RenderFileDiff renders a full file diff with header and collapsed unchanged regions.
func (vd *VisualDiff) RenderFileDiff(path, oldContent, newContent string) string {
	var out strings.Builder

	// Header
	out.WriteString(vd.Theme.Header + "─── " + path + " ───" + vd.Theme.Reset + "\n")

	oldLines := strings.Split(oldContent, "\n")
	newLines := strings.Split(newContent, "\n")

	// Simple line-by-line diff
	edits := computeLineDiff(oldLines, newLines)

	// Determine which output lines are "interesting" (changed)
	type outputLine struct {
		lineType byte // '+', '-', ' '
		text     string
		oldNum   int
		newNum   int
	}
	var outputLines []outputLine

	oldIdx, newIdx := 0, 0
	for _, ed := range edits {
		switch ed {
		case 'e':
			outputLines = append(outputLines, outputLine{lineType: ' ', text: oldLines[oldIdx], oldNum: oldIdx + 1, newNum: newIdx + 1})
			oldIdx++
			newIdx++
		case 'd':
			outputLines = append(outputLines, outputLine{lineType: '-', text: oldLines[oldIdx], oldNum: oldIdx + 1})
			oldIdx++
		case 'i':
			outputLines = append(outputLines, outputLine{lineType: '+', text: newLines[newIdx], newNum: newIdx + 1})
			newIdx++
		}
	}

	// Collapse context: only show ContextLines around changes
	contextLines := vd.ContextLines
	visible := make([]bool, len(outputLines))
	for i, ol := range outputLines {
		if ol.lineType != ' ' {
			// Mark surrounding context as visible
			for j := max(0, i-contextLines); j <= min(len(outputLines)-1, i+contextLines); j++ {
				visible[j] = true
			}
		}
	}

	i := 0
	for i < len(outputLines) {
		if !visible[i] {
			// Count collapsed lines
			start := i
			for i < len(outputLines) && !visible[i] {
				i++
			}
			collapsed := i - start
			out.WriteString(fmt.Sprintf("%s... %d lines ...%s\n", vd.Theme.Context, collapsed, vd.Theme.Reset))
			continue
		}
		ol := outputLines[i]
		switch ol.lineType {
		case '-':
			if vd.ShowLineNumbers {
				out.WriteString(fmt.Sprintf("%s%4d%s %s-%s%s\n",
					vd.Theme.LineNo, ol.oldNum, vd.Theme.Reset,
					vd.Theme.Removed, ol.text, vd.Theme.Reset))
			} else {
				out.WriteString(fmt.Sprintf("%s-%s%s\n", vd.Theme.Removed, ol.text, vd.Theme.Reset))
			}
		case '+':
			if vd.ShowLineNumbers {
				out.WriteString(fmt.Sprintf("%s%4d%s %s+%s%s\n",
					vd.Theme.LineNo, ol.newNum, vd.Theme.Reset,
					vd.Theme.Added, ol.text, vd.Theme.Reset))
			} else {
				out.WriteString(fmt.Sprintf("%s+%s%s\n", vd.Theme.Added, ol.text, vd.Theme.Reset))
			}
		default:
			if vd.ShowLineNumbers {
				out.WriteString(fmt.Sprintf("%s%4d%s  %s\n",
					vd.Theme.LineNo, ol.newNum, vd.Theme.Reset, ol.text))
			} else {
				out.WriteString(fmt.Sprintf(" %s\n", ol.text))
			}
		}
		i++
	}

	return out.String()
}

// RenderDiffSummary renders a summary table of file changes.
func (vd *VisualDiff) RenderDiffSummary(files []FileDiffStat) string {
	if len(files) == 0 {
		return ""
	}

	var out strings.Builder

	out.WriteString(fmt.Sprintf("Files changed: %d\n", len(files)))
	out.WriteString("──────────────────────────────────\n")

	// Find max additions for scaling the bar
	maxChanges := 0
	for _, f := range files {
		total := f.Additions + f.Deletions
		if total > maxChanges {
			maxChanges = total
		}
	}

	// Find max path length for alignment
	maxPathLen := 0
	for _, f := range files {
		if len(f.Path) > maxPathLen {
			maxPathLen = len(f.Path)
		}
	}
	if maxPathLen > 30 {
		maxPathLen = 30
	}

	barMaxWidth := 13

	for _, f := range files {
		path := f.Path
		if len(path) > maxPathLen {
			path = path[len(path)-maxPathLen:]
		}

		// Format change counts
		var changeStr string
		switch {
		case f.Additions > 0 && f.Deletions > 0:
			changeStr = fmt.Sprintf("+%d -%d", f.Additions, f.Deletions)
		case f.Additions > 0:
			changeStr = fmt.Sprintf("+%d", f.Additions)
		case f.Deletions > 0:
			changeStr = fmt.Sprintf("-%d", f.Deletions)
		default:
			changeStr = "0"
		}

		// Build bar
		total := f.Additions + f.Deletions
		barLen := 0
		if maxChanges > 0 {
			barLen = (total * barMaxWidth) / maxChanges
		}
		if barLen < 1 && total > 0 {
			barLen = 1
		}

		addBars := 0
		delBars := 0
		if total > 0 {
			addBars = (f.Additions * barLen) / total
			delBars = barLen - addBars
		}

		bar := vd.Theme.Added + strings.Repeat("█", addBars) + vd.Theme.Reset +
			vd.Theme.Removed + strings.Repeat("█", delBars) + vd.Theme.Reset

		out.WriteString(fmt.Sprintf("%s %-*s %7s %s\n",
			f.Status, maxPathLen, path, changeStr, bar))
	}

	out.WriteString("──────────────────────────────────\n")

	totalAdd, totalDel := 0, 0
	for _, f := range files {
		totalAdd += f.Additions
		totalDel += f.Deletions
	}
	out.WriteString(fmt.Sprintf("Total: +%d -%d\n", totalAdd, totalDel))

	return out.String()
}

// ColorizeByLanguage provides basic syntax highlighting for a line based on language.
func (vd *VisualDiff) ColorizeByLanguage(line, language string) string {
	keywords := map[string][]string{
		"go": {
			"func", "package", "import", "var", "const", "type", "struct",
			"interface", "map", "chan", "go", "defer", "return", "if", "else",
			"for", "range", "switch", "case", "default", "select", "break",
			"continue", "fallthrough", "nil", "true", "false",
		},
		"python": {
			"def", "class", "import", "from", "return", "if", "elif", "else",
			"for", "while", "try", "except", "finally", "with", "as", "yield",
			"lambda", "pass", "break", "continue", "None", "True", "False",
		},
		"javascript": {
			"function", "const", "let", "var", "return", "if", "else", "for",
			"while", "class", "extends", "import", "export", "default", "async",
			"await", "try", "catch", "finally", "throw", "new", "this",
			"null", "undefined", "true", "false",
		},
		"rust": {
			"fn", "let", "mut", "pub", "struct", "enum", "impl", "trait",
			"use", "mod", "crate", "self", "super", "return", "if", "else",
			"for", "while", "loop", "match", "break", "continue",
			"true", "false", "None", "Some",
		},
	}

	kw, ok := keywords[strings.ToLower(language)]
	if !ok {
		return line
	}

	keywordColor := "\033[34;1m" // blue+bold
	stringColor := "\033[33m"    // yellow
	commentColor := "\033[2;32m" // dim green
	reset := vd.Theme.Reset

	// Highlight single-line comments
	commentPrefixes := map[string]string{
		"go": "//", "python": "#", "javascript": "//", "rust": "//",
	}
	if cp, exists := commentPrefixes[strings.ToLower(language)]; exists {
		if idx := strings.Index(line, cp); idx >= 0 {
			before := line[:idx]
			comment := line[idx:]
			return vd.colorizeWords(before, kw, keywordColor, stringColor, reset) +
				commentColor + comment + reset
		}
	}

	return vd.colorizeWords(line, kw, keywordColor, stringColor, reset)
}

// colorizeWords highlights keywords and strings in a line.
func (vd *VisualDiff) colorizeWords(line string, keywords []string, keywordColor, stringColor, reset string) string {
	words := splitPreservingSpaces(line)
	var out strings.Builder

	kwSet := make(map[string]bool, len(keywords))
	for _, k := range keywords {
		kwSet[k] = true
	}

	inString := false
	stringChar := byte(0)

	for _, w := range words {
		if inString {
			out.WriteString(stringColor + w + reset)
			if len(w) > 0 && w[len(w)-1] == stringChar {
				inString = false
			}
			continue
		}

		if len(w) > 0 && (w[0] == '"' || w[0] == '\'' || w[0] == '`') {
			inString = true
			stringChar = w[0]
			out.WriteString(stringColor + w + reset)
			if len(w) > 1 && w[len(w)-1] == stringChar {
				inString = false
			}
			continue
		}

		// Strip punctuation for keyword matching
		cleaned := strings.TrimRight(w, "({[,;:.")
		if kwSet[cleaned] {
			out.WriteString(keywordColor + cleaned + reset + w[len(cleaned):])
		} else {
			out.WriteString(w)
		}
	}

	return out.String()
}

// --- Helper functions ---

// parseHunkHeader parses a unified diff hunk header like "@@ -1,5 +1,6 @@"
func parseHunkHeader(header string) (oldStart, newStart int) {
	// Format: @@ -old,count +new,count @@
	oldStart, newStart = 1, 1
	parts := strings.Split(header, " ")
	for _, p := range parts {
		if strings.HasPrefix(p, "-") && strings.Contains(p, ",") {
			_, _ = fmt.Sscanf(p, "-%d,", &oldStart)
		} else if strings.HasPrefix(p, "-") && len(p) > 1 && p[1] >= '0' && p[1] <= '9' {
			_, _ = fmt.Sscanf(p, "-%d", &oldStart)
		}
		if strings.HasPrefix(p, "+") && strings.Contains(p, ",") {
			_, _ = fmt.Sscanf(p, "+%d,", &newStart)
		} else if strings.HasPrefix(p, "+") && len(p) > 1 && p[1] >= '0' && p[1] <= '9' {
			_, _ = fmt.Sscanf(p, "+%d", &newStart)
		}
	}
	return
}

// splitWords splits a string into words, preserving whitespace as separate tokens.
func splitWords(s string) []string {
	var words []string
	current := strings.Builder{}
	inSpace := false

	for _, r := range s {
		isSpace := r == ' ' || r == '\t'
		if isSpace {
			if !inSpace && current.Len() > 0 {
				words = append(words, current.String())
				current.Reset()
			}
			inSpace = true
			current.WriteRune(r)
		} else {
			if inSpace && current.Len() > 0 {
				words = append(words, current.String())
				current.Reset()
			}
			inSpace = false
			current.WriteRune(r)
		}
	}
	if current.Len() > 0 {
		words = append(words, current.String())
	}
	return words
}

// splitPreservingSpaces splits a line into tokens preserving spaces as separate tokens.
func splitPreservingSpaces(s string) []string {
	var tokens []string
	current := strings.Builder{}

	for i := 0; i < len(s); i++ {
		if s[i] == ' ' || s[i] == '\t' {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
			tokens = append(tokens, string(s[i]))
		} else {
			current.WriteByte(s[i])
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

// computeLCS computes the longest common subsequence of two string slices.
func computeLCS(a, b []string) []string {
	m, n := len(a), len(b)
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}

	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else {
				dp[i][j] = max(dp[i-1][j], dp[i][j-1])
			}
		}
	}

	// Backtrack
	lcs := make([]string, 0, dp[m][n])
	i, j := m, n
	for i > 0 && j > 0 {
		if a[i-1] == b[j-1] {
			lcs = append(lcs, a[i-1])
			i--
			j--
		} else if dp[i-1][j] > dp[i][j-1] {
			i--
		} else {
			j--
		}
	}

	// Reverse
	for left, right := 0, len(lcs)-1; left < right; left, right = left+1, right-1 {
		lcs[left], lcs[right] = lcs[right], lcs[left]
	}
	return lcs
}

// computeLineDiff computes edit operations between two slices of lines.
// Returns a slice of bytes: 'e' (equal), 'd' (delete from old), 'i' (insert from new).
func computeLineDiff(old, new []string) []byte {
	m, n := len(old), len(new)

	// Use LCS approach for lines
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if old[i-1] == new[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else {
				dp[i][j] = max(dp[i-1][j], dp[i][j-1])
			}
		}
	}

	// Backtrack to get edit operations
	var ops []byte
	i, j := m, n
	for i > 0 || j > 0 {
		if i > 0 && j > 0 && old[i-1] == new[j-1] {
			ops = append(ops, 'e')
			i--
			j--
		} else if j > 0 && (i == 0 || dp[i][j-1] >= dp[i-1][j]) {
			ops = append(ops, 'i')
			j--
		} else {
			ops = append(ops, 'd')
			i--
		}
	}

	// Reverse
	for left, right := 0, len(ops)-1; left < right; left, right = left+1, right-1 {
		ops[left], ops[right] = ops[right], ops[left]
	}
	return ops
}

// padOrTruncate ensures a string is exactly the given width (for column alignment).
func padOrTruncate(s string, width int) string {
	// Use visible length (stripping ANSI)
	visLen := visibleLength(s)
	if visLen >= width {
		return truncateVisible(s, width)
	}
	return s + strings.Repeat(" ", width-visLen)
}

// vdStripAnsi removes ANSI escape codes from a string.
func vdStripAnsi(s string) string {
	var out strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == '\033' {
			// Skip to 'm'
			for i < len(s) && s[i] != 'm' {
				i++
			}
			if i < len(s) {
				i++ // skip 'm'
			}
		} else {
			out.WriteByte(s[i])
			i++
		}
	}
	return out.String()
}

// visibleLength returns the visible length of a string (excluding ANSI codes).
func visibleLength(s string) int {
	return runewidth.StringWidth(vdStripAnsi(s))
}

// truncateVisible truncates a string with ANSI codes to a visible width.
func truncateVisible(s string, width int) string {
	var out strings.Builder
	visible := 0
	i := 0
	for i < len(s) && visible < width {
		if s[i] == '\033' {
			// Copy entire escape sequence
			for i < len(s) && s[i] != 'm' {
				out.WriteByte(s[i])
				i++
			}
			if i < len(s) {
				out.WriteByte(s[i])
				i++
			}
		} else {
			r, size := utf8.DecodeRuneInString(s[i:])
			rw := runewidth.RuneWidth(r)
			if visible+rw > width {
				break
			}
			out.WriteString(s[i : i+size])
			visible += rw
			i += size
		}
	}
	return out.String()
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
