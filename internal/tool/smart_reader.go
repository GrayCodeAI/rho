package tool

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/GrayCodeAI/rho/internal/token"
)

// ──────────────────────────────────────────────────────────────────────────────
// SmartReader: token-aware file reader that intelligently reads files within a
// token budget, showing the most relevant parts when files are too large.
// ──────────────────────────────────────────────────────────────────────────────

const defaultMaxTokens = 8000

// SmartReader reads files within a token budget using adaptive strategies.
type SmartReader struct {
	MaxTokens int
	Strategy  string // "full", "head_tail", "symbols", "relevant"
	mu        sync.Mutex
}

// ReadResult contains the result of a smart file read.
type ReadResult struct {
	Content    string
	Tokens     int
	Truncated  bool
	Strategy   string
	Sections   []ReadSection
	TotalLines int
	ShownLines int
}

// ReadSection represents a contiguous section of a file that was read.
type ReadSection struct {
	StartLine int
	EndLine   int
	Content   string
	Reason    string // "head", "tail", "function", "relevant", "import"
}

// NewSmartReader creates a SmartReader with the given token budget.
func NewSmartReader(maxTokens int) *SmartReader {
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}
	return &SmartReader{
		MaxTokens: maxTokens,
		Strategy:  "relevant",
	}
}

func estimateTokens(text string) int {
	return token.CountTokensFast(text)
}

// ReadFile reads a file intelligently within the token budget.
// If the file fits in budget, the full content is returned.
// Otherwise the configured strategy is applied.
func (sr *SmartReader) ReadFile(path string, query string) (*ReadResult, error) {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	data, err := os.ReadFile(path) // #nosec G304 -- path provided by caller via tool/task parameters, inherent to this dev CLI's file operations
	if err != nil {
		return nil, fmt.Errorf("smart_reader: %w", err)
	}
	if IsBinaryContent(data) {
		return nil, fmt.Errorf("smart_reader: binary file not supported: %s", path)
	}
	content := string(StripBOM(data))
	lines := strings.Split(content, "\n")
	totalLines := len(lines)
	tokens := estimateTokens(content)

	// If file fits within budget, return full content.
	if tokens <= sr.MaxTokens {
		return &ReadResult{
			Content:    content,
			Tokens:     tokens,
			Truncated:  false,
			Strategy:   "full",
			TotalLines: totalLines,
			ShownLines: totalLines,
			Sections: []ReadSection{{
				StartLine: 1,
				EndLine:   totalLines,
				Content:   content,
				Reason:    "head",
			}},
		}, nil
	}

	// File exceeds budget — apply strategy.
	strategy := sr.Strategy
	if strategy == "" {
		strategy = "relevant"
	}

	switch strategy {
	case "head_tail":
		return sr.readHeadTail(lines, totalLines)
	case "symbols":
		return sr.readSymbols(lines, totalLines, path)
	case "relevant":
		if query == "" {
			return sr.readHeadTail(lines, totalLines)
		}
		return sr.readRelevantLines(lines, totalLines, query, sr.MaxTokens)
	default:
		return sr.readHeadTail(lines, totalLines)
	}
}

// ReadWithBudget reads a file within an exact token budget using adaptive strategy.
func (sr *SmartReader) ReadWithBudget(path string, budget int) (*ReadResult, error) {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	if budget <= 0 {
		budget = sr.MaxTokens
	}

	data, err := os.ReadFile(path) // #nosec G304 -- path provided by caller via tool/task parameters, inherent to this dev CLI's file operations
	if err != nil {
		return nil, fmt.Errorf("smart_reader: %w", err)
	}
	if IsBinaryContent(data) {
		return nil, fmt.Errorf("smart_reader: binary file not supported: %s", path)
	}
	content := string(StripBOM(data))
	lines := strings.Split(content, "\n")
	totalLines := len(lines)
	tokens := estimateTokens(content)

	if tokens <= budget {
		return &ReadResult{
			Content:    content,
			Tokens:     tokens,
			Truncated:  false,
			Strategy:   "full",
			TotalLines: totalLines,
			ShownLines: totalLines,
			Sections: []ReadSection{{
				StartLine: 1,
				EndLine:   totalLines,
				Content:   content,
				Reason:    "head",
			}},
		}, nil
	}

	// Calculate how many lines we can fit at approximately 4 chars per token.
	// Use head_tail approach but scale to budget.
	avgLineLen := len(content) / totalLines
	tokensPerLine := (avgLineLen + 3) / 4
	if tokensPerLine < 1 {
		tokensPerLine = 1
	}
	maxLines := budget / tokensPerLine
	headLines := maxLines * 2 / 3
	tailLines := maxLines - headLines
	if headLines < 1 {
		headLines = 1
	}
	if tailLines < 1 {
		tailLines = 1
	}
	if headLines > totalLines {
		headLines = totalLines
	}
	if tailLines > totalLines-headLines {
		tailLines = totalLines - headLines
	}

	var sections []ReadSection
	var resultContent strings.Builder
	shownLines := 0

	// Head section
	headEnd := headLines
	headContent := strings.Join(lines[:headEnd], "\n")
	sections = append(sections, ReadSection{
		StartLine: 1,
		EndLine:   headEnd,
		Content:   headContent,
		Reason:    "head",
	})
	resultContent.WriteString(headContent)
	resultContent.WriteString("\n")
	shownLines += headEnd

	// Tail section
	if tailLines > 0 && totalLines-tailLines > headEnd {
		tailStart := totalLines - tailLines
		tailContent := strings.Join(lines[tailStart:], "\n")
		sections = append(sections, ReadSection{
			StartLine: tailStart + 1,
			EndLine:   totalLines,
			Content:   tailContent,
			Reason:    "tail",
		})
		resultContent.WriteString("\n... (")
		resultContent.WriteString(fmt.Sprintf("%d lines omitted", tailStart-headEnd))
		resultContent.WriteString(") ...\n\n")
		resultContent.WriteString(tailContent)
		shownLines += tailLines
	}

	result := resultContent.String()
	return &ReadResult{
		Content:    result,
		Tokens:     estimateTokens(result),
		Truncated:  true,
		Strategy:   "head_tail",
		Sections:   sections,
		TotalLines: totalLines,
		ShownLines: shownLines,
	}, nil
}

// ReadSymbolsOnly extracts function signatures, type definitions, and exports.
func (sr *SmartReader) ReadSymbolsOnly(path string) (*ReadResult, error) {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	data, err := os.ReadFile(path) // #nosec G304 -- path provided by caller via tool/task parameters, inherent to this dev CLI's file operations
	if err != nil {
		return nil, fmt.Errorf("smart_reader: %w", err)
	}
	if IsBinaryContent(data) {
		return nil, fmt.Errorf("smart_reader: binary file not supported: %s", path)
	}
	content := string(StripBOM(data))
	lines := strings.Split(content, "\n")
	totalLines := len(lines)

	return sr.readSymbols(lines, totalLines, path)
}

// ReadRelevant reads lines containing query keywords with surrounding context.
func (sr *SmartReader) ReadRelevant(path, query string, budget int) (*ReadResult, error) {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	if budget <= 0 {
		budget = sr.MaxTokens
	}

	data, err := os.ReadFile(path) // #nosec G304 -- path provided by caller via tool/task parameters, inherent to this dev CLI's file operations
	if err != nil {
		return nil, fmt.Errorf("smart_reader: %w", err)
	}
	if IsBinaryContent(data) {
		return nil, fmt.Errorf("smart_reader: binary file not supported: %s", path)
	}
	content := string(StripBOM(data))
	lines := strings.Split(content, "\n")
	totalLines := len(lines)
	tokens := estimateTokens(content)

	if tokens <= budget {
		return &ReadResult{
			Content:    content,
			Tokens:     tokens,
			Truncated:  false,
			Strategy:   "full",
			TotalLines: totalLines,
			ShownLines: totalLines,
			Sections: []ReadSection{{
				StartLine: 1,
				EndLine:   totalLines,
				Content:   content,
				Reason:    "head",
			}},
		}, nil
	}

	return sr.readRelevantLines(lines, totalLines, query, budget)
}

// ReadRange reads a specific line range from a file.
func (sr *SmartReader) ReadRange(path string, startLine, endLine int) (*ReadResult, error) {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	f, err := os.Open(path) // #nosec G304 -- path provided by caller via tool/task parameters, inherent to this dev CLI's file operations
	if err != nil {
		return nil, fmt.Errorf("smart_reader: %w", err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	var lines []string
	lineNum := 0
	totalLines := 0
	for scanner.Scan() {
		totalLines++
		lineNum++
		if lineNum >= startLine && (endLine <= 0 || lineNum <= endLine) {
			lines = append(lines, scanner.Text())
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("smart_reader: scanning %s: %w", path, err)
	}

	if startLine > totalLines {
		return nil, fmt.Errorf("smart_reader: start_line %d exceeds file length %d", startLine, totalLines)
	}

	actualEnd := endLine
	if endLine <= 0 || endLine > totalLines {
		actualEnd = totalLines
	}

	content := strings.Join(lines, "\n")
	shownLines := len(lines)

	return &ReadResult{
		Content:    content,
		Tokens:     estimateTokens(content),
		Truncated:  shownLines < totalLines,
		Strategy:   "range",
		TotalLines: totalLines,
		ShownLines: shownLines,
		Sections: []ReadSection{{
			StartLine: startLine,
			EndLine:   actualEnd,
			Content:   content,
			Reason:    "relevant",
		}},
	}, nil
}

// EstimateFileTokens quickly estimates the token count without reading the full file.
// Uses file size / 4 as approximation.
func (sr *SmartReader) EstimateFileTokens(path string) (int, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("smart_reader: %w", err)
	}
	return int(info.Size()) / 4, nil
}

// FormatResult formats a ReadResult for display.
func FormatResult(path string, result *ReadResult) string {
	var b strings.Builder

	_, _ = fmt.Fprintf(&b, "%s (%d lines, showing %d):\n", path, result.TotalLines, result.ShownLines)

	for _, sec := range result.Sections {
		reason := sec.Reason
		if reason != "" {
			reason = " (" + reason + ")"
		}
		_, _ = fmt.Fprintf(&b, "[%d-%d]%s\n", sec.StartLine, sec.EndLine, reason)
	}

	if result.Truncated {
		omitted := result.TotalLines - result.ShownLines
		_, _ = fmt.Fprintf(&b, "\nTruncated: %d lines omitted (symbols-only available via /symbols)\n", omitted)
	}

	return b.String()
}

// ──────────────────────────────────────────────────────────────────────────────
// Internal helpers
// ──────────────────────────────────────────────────────────────────────────────

// readHeadTail returns the first 50 and last 50 lines.
func (sr *SmartReader) readHeadTail(lines []string, totalLines int) (*ReadResult, error) {
	headCount := 50
	tailCount := 50
	if totalLines <= headCount+tailCount {
		content := strings.Join(lines, "\n")
		return &ReadResult{
			Content:    content,
			Tokens:     estimateTokens(content),
			Truncated:  false,
			Strategy:   "head_tail",
			TotalLines: totalLines,
			ShownLines: totalLines,
			Sections: []ReadSection{{
				StartLine: 1,
				EndLine:   totalLines,
				Content:   content,
				Reason:    "head",
			}},
		}, nil
	}

	var sections []ReadSection
	var resultContent strings.Builder

	// Head
	headContent := strings.Join(lines[:headCount], "\n")
	sections = append(sections, ReadSection{
		StartLine: 1,
		EndLine:   headCount,
		Content:   headContent,
		Reason:    "head",
	})
	resultContent.WriteString(headContent)
	resultContent.WriteString("\n\n... (")
	resultContent.WriteString(fmt.Sprintf("%d lines omitted", totalLines-headCount-tailCount))
	resultContent.WriteString(") ...\n\n")

	// Tail
	tailStart := totalLines - tailCount
	tailContent := strings.Join(lines[tailStart:], "\n")
	sections = append(sections, ReadSection{
		StartLine: tailStart + 1,
		EndLine:   totalLines,
		Content:   tailContent,
		Reason:    "tail",
	})
	resultContent.WriteString(tailContent)

	result := resultContent.String()
	shownLines := headCount + tailCount

	return &ReadResult{
		Content:    result,
		Tokens:     estimateTokens(result),
		Truncated:  true,
		Strategy:   "head_tail",
		Sections:   sections,
		TotalLines: totalLines,
		ShownLines: shownLines,
	}, nil
}

// Symbol extraction regexps (language-agnostic best-effort).
var symbolPatterns = []*regexp.Regexp{
	// Go: func, type, const, var, interface
	regexp.MustCompile(`^\s*(func\s+(\([^)]*\)\s*)?[A-Za-z_]\w*)`),
	regexp.MustCompile(`^\s*type\s+[A-Za-z_]\w*\s+`),
	regexp.MustCompile(`^\s*(var|const)\s+[A-Za-z_]\w*`),
	// Python: def, class
	regexp.MustCompile(`^\s*(def|class)\s+[A-Za-z_]\w*`),
	// JS/TS: function, export, class
	regexp.MustCompile(`^\s*(export\s+)?(function|class|const|let|var)\s+[A-Za-z_]\w*`),
	regexp.MustCompile(`^\s*(export\s+)(default\s+)?(function|class)`),
	// Rust: fn, struct, enum, impl, trait, pub
	regexp.MustCompile(`^\s*(pub\s+)?(fn|struct|enum|impl|trait|mod)\s+[A-Za-z_]\w*`),
	// Java/C#: public/private/protected class/interface/void/...
	regexp.MustCompile(`^\s*(public|private|protected)\s+.*\s+[A-Za-z_]\w*\s*\(`),
	// Import/package lines
	regexp.MustCompile(`^\s*(import|package|from|require|use)\s+`),
}

// readSymbols extracts symbol definitions from lines.
func (sr *SmartReader) readSymbols(lines []string, totalLines int, path string) (*ReadResult, error) {
	var sections []ReadSection
	var resultContent strings.Builder
	shownLines := 0

	// Always include first few lines (package/imports).
	importEnd := 0
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if i > 0 && trimmed == "" && importEnd == 0 {
			// First blank line after start could be end of imports.
			continue
		}
		if i > 20 {
			break
		}
		isImport := strings.HasPrefix(trimmed, "import") ||
			strings.HasPrefix(trimmed, "package") ||
			strings.HasPrefix(trimmed, "from ") ||
			strings.HasPrefix(trimmed, "require") ||
			strings.HasPrefix(trimmed, "use ") ||
			strings.HasPrefix(trimmed, "#include") ||
			trimmed == "" || trimmed == ")" || trimmed == "("
		if isImport || i < 3 {
			importEnd = i + 1
		}
	}
	if importEnd > 0 {
		importContent := strings.Join(lines[:importEnd], "\n")
		sections = append(sections, ReadSection{
			StartLine: 1,
			EndLine:   importEnd,
			Content:   importContent,
			Reason:    "import",
		})
		resultContent.WriteString(importContent)
		resultContent.WriteString("\n\n")
		shownLines += importEnd
	}

	// Find symbol lines.
	inBlock := false
	blockStart := -1
	for i := importEnd; i < totalLines; i++ {
		line := lines[i]
		isSymbol := false
		for _, pat := range symbolPatterns {
			if pat.MatchString(line) && !strings.HasPrefix(strings.TrimSpace(line), "import") {
				isSymbol = true
				break
			}
		}
		if isSymbol {
			if !inBlock {
				blockStart = i
				inBlock = true
			}
		} else if inBlock {
			// End the block: include just the signature line(s).
			blockContent := strings.Join(lines[blockStart:i], "\n")
			sections = append(sections, ReadSection{
				StartLine: blockStart + 1,
				EndLine:   i,
				Content:   blockContent,
				Reason:    "function",
			})
			resultContent.WriteString(blockContent)
			resultContent.WriteString("\n")
			shownLines += i - blockStart
			inBlock = false
		}
	}
	// Close any trailing block.
	if inBlock {
		blockContent := strings.Join(lines[blockStart:], "\n")
		sections = append(sections, ReadSection{
			StartLine: blockStart + 1,
			EndLine:   totalLines,
			Content:   blockContent,
			Reason:    "function",
		})
		resultContent.WriteString(blockContent)
		resultContent.WriteString("\n")
		shownLines += totalLines - blockStart
	}

	result := resultContent.String()
	return &ReadResult{
		Content:    result,
		Tokens:     estimateTokens(result),
		Truncated:  shownLines < totalLines,
		Strategy:   "symbols",
		Sections:   sections,
		TotalLines: totalLines,
		ShownLines: shownLines,
	}, nil
}

// readRelevantLines finds lines matching query keywords with 3 lines of context.
func (sr *SmartReader) readRelevantLines(lines []string, totalLines int, query string, budget int) (*ReadResult, error) {
	keywords := srExtractKeywords(query)
	if len(keywords) == 0 {
		return sr.readHeadTail(lines, totalLines)
	}

	// Score each line.
	type scoredLine struct {
		index int
		score int
		isFn  bool
	}
	var matches []scoredLine
	for i, line := range lines {
		lower := strings.ToLower(line)
		score := 0
		for _, kw := range keywords {
			if strings.Contains(lower, kw) {
				score++
			}
		}
		if score > 0 {
			isFn := false
			for _, pat := range symbolPatterns[:5] { // Check Go/Python/JS patterns
				if pat.MatchString(line) {
					isFn = true
					score += 2 // Boost function definitions.
					break
				}
			}
			matches = append(matches, scoredLine{index: i, score: score, isFn: isFn})
		}
	}

	if len(matches) == 0 {
		return sr.readHeadTail(lines, totalLines)
	}

	// Sort by score descending (simple insertion sort to avoid sort import).
	for i := 1; i < len(matches); i++ {
		for j := i; j > 0 && matches[j].score > matches[j-1].score; j-- {
			matches[j], matches[j-1] = matches[j-1], matches[j]
		}
	}

	// Build sections with 3 lines of context, respecting budget.
	included := make(map[int]bool)
	contextRadius := 3
	usedTokens := 0

	var sections []ReadSection

	for _, m := range matches {
		start := m.index - contextRadius
		if start < 0 {
			start = 0
		}
		end := m.index + contextRadius + 1
		if end > totalLines {
			end = totalLines
		}

		// Check if adding this section would exceed budget.
		sectionContent := strings.Join(lines[start:end], "\n")
		sectionTokens := estimateTokens(sectionContent)
		if usedTokens+sectionTokens > budget && len(sections) > 0 {
			break
		}

		// Check overlap with existing included lines.
		newLines := false
		for i := start; i < end; i++ {
			if !included[i] {
				newLines = true
				break
			}
		}
		if !newLines {
			continue
		}

		for i := start; i < end; i++ {
			included[i] = true
		}

		reason := "relevant"
		if m.isFn {
			reason = "function"
		}

		sections = append(sections, ReadSection{
			StartLine: start + 1,
			EndLine:   end,
			Content:   sectionContent,
			Reason:    reason,
		})
		usedTokens += sectionTokens
	}

	// Merge overlapping/adjacent sections.
	sections = mergeSections(sections, lines)

	// Build final content.
	var resultContent strings.Builder
	shownLines := 0
	for i, sec := range sections {
		if i > 0 {
			resultContent.WriteString("\n...\n\n")
		}
		resultContent.WriteString(sec.Content)
		resultContent.WriteString("\n")
		shownLines += sec.EndLine - sec.StartLine + 1
	}

	result := resultContent.String()
	return &ReadResult{
		Content:    result,
		Tokens:     estimateTokens(result),
		Truncated:  shownLines < totalLines,
		Strategy:   "relevant",
		Sections:   sections,
		TotalLines: totalLines,
		ShownLines: shownLines,
	}, nil
}

// srExtractKeywords splits query into lowercase keyword tokens for smart reader.
func srExtractKeywords(query string) []string {
	words := strings.Fields(strings.ToLower(query))
	var keywords []string
	// Filter out very short or common stop words.
	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "are": true,
		"in": true, "of": true, "to": true, "for": true, "and": true,
		"or": true, "it": true, "this": true, "that": true, "with": true,
	}
	for _, w := range words {
		if len(w) >= 2 && !stopWords[w] {
			keywords = append(keywords, w)
		}
	}
	return keywords
}

// mergeSections merges overlapping or adjacent sections.
func mergeSections(sections []ReadSection, lines []string) []ReadSection {
	if len(sections) <= 1 {
		return sections
	}

	// Sort by StartLine (insertion sort).
	for i := 1; i < len(sections); i++ {
		for j := i; j > 0 && sections[j].StartLine < sections[j-1].StartLine; j-- {
			sections[j], sections[j-1] = sections[j-1], sections[j]
		}
	}

	var merged []ReadSection
	current := sections[0]
	for i := 1; i < len(sections); i++ {
		next := sections[i]
		// If overlapping or adjacent (within 2 lines), merge.
		if next.StartLine <= current.EndLine+2 {
			if next.EndLine > current.EndLine {
				current.EndLine = next.EndLine
				current.Content = strings.Join(lines[current.StartLine-1:current.EndLine], "\n")
			}
			// Keep the more descriptive reason.
			if next.Reason == "function" {
				current.Reason = "function"
			}
		} else {
			merged = append(merged, current)
			current = next
		}
	}
	merged = append(merged, current)
	return merged
}

// ──────────────────────────────────────────────────────────────────────────────
// SmartReaderTool: Tool interface implementation
// ──────────────────────────────────────────────────────────────────────────────

// SmartReaderTool exposes SmartReader as a rho tool.
type SmartReaderTool struct {
	reader *SmartReader
}

func NewSmartReaderTool() *SmartReaderTool {
	return &SmartReaderTool{reader: NewSmartReader(defaultMaxTokens)}
}

func (SmartReaderTool) Name() string { return "SmartRead" }

// SmartReaderInput is the typed input for SmartReaderTool.
type SmartReaderInput struct {
	Path      string `json:"path"`
	Query     string `json:"query"`
	Strategy  string `json:"strategy"`
	Budget    int    `json:"budget"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

func (SmartReaderTool) RiskLevel() string { return "low" }
func (SmartReaderTool) Aliases() []string { return []string{"smart_read"} }

func (SmartReaderTool) Description() string {
	return `Token-aware file reader that intelligently reads files within a token budget. Shows full content for small files, or the most relevant parts (function signatures, keyword matches with context, head/tail) for large files. Use when you need to understand a large file without reading all of it.`
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (SmartReaderTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"path":       {Type: "string", Description: "File path to read."},
			"query":      {Type: "string", Description: "Optional query to prioritize relevant sections (keywords that appear in the file)."},
			"strategy":   {Type: "string", Enum: []interface{}{"full", "head_tail", "symbols", "relevant"}, Description: "Reading strategy. Default: relevant (auto-selects head_tail if no query)."},
			"budget":     {Type: "integer", Description: "Max token budget for reading (default 8000)."},
			"start_line": {Type: "integer", Description: "Read from this line (1-based). If set, reads a specific range."},
			"end_line":   {Type: "integer", Description: "Read to this line (1-based, inclusive). Used with start_line."},
		},
		Required: []string{"path"},
	}
}

func (SmartReaderTool) Parameters() map[string]interface{} {
	return smartReaderSchema.ToJSONSchema()
}

// smartReaderSchema is the single source of truth for SmartReader's input schema.
var smartReaderSchema = SmartReaderTool{}.Schema()

func (t *SmartReaderTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[SmartReaderInput]("SmartReader", input)
	if err != nil {
		return "", err
	}
	if p.Path == "" {
		return "", fmt.Errorf("path is required")
	}
	if err := validatePathAllowed(ctx, p.Path); err != nil {
		return "", err
	}
	if reason := IsSensitivePath(p.Path); reason != "" {
		return "", fmt.Errorf("blocked: %s", reason)
	}

	// Range read takes priority.
	if p.StartLine > 0 {
		result, err := t.reader.ReadRange(p.Path, p.StartLine, p.EndLine)
		if err != nil {
			return "", err
		}
		return FormatResult(p.Path, result) + "\n" + result.Content, nil
	}

	// Apply strategy override if specified.
	reader := t.reader
	if p.Strategy != "" || p.Budget > 0 {
		budget := p.Budget
		if budget <= 0 {
			budget = reader.MaxTokens
		}
		reader = NewSmartReader(budget)
		if p.Strategy != "" {
			reader.Strategy = p.Strategy
		}
	}

	result, err := reader.ReadFile(p.Path, p.Query)
	if err != nil {
		return "", err
	}

	return FormatResult(p.Path, result) + "\n" + result.Content, nil
}
