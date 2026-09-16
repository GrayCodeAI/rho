package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
)

// ConflictResolver provides intelligent git merge conflict resolution.
type ConflictResolver struct {
	Strategy string // "ours", "theirs", "smart"
	mu       sync.Mutex
}

// Conflict represents a single conflict region within a file.
type Conflict struct {
	File          string
	StartLine     int
	EndLine       int
	OursContent   string
	TheirsContent string
	BaseContent   string
	Resolved      bool
	Resolution    string
}

// ConflictFile represents a file containing one or more merge conflicts.
type ConflictFile struct {
	Path        string
	Conflicts   []Conflict
	FullContent string
}

// NewConflictResolver creates a ConflictResolver with the "smart" strategy.
func NewConflictResolver() *ConflictResolver {
	return &ConflictResolver{
		Strategy: "smart",
	}
}

// ParseConflicts reads a file and extracts all conflict regions.
func (cr *ConflictResolver) ParseConflicts(path string) (*ConflictFile, error) {
	cr.mu.Lock()
	defer cr.mu.Unlock()

	data, err := os.ReadFile(path) // #nosec G304 -- path provided by caller via tool/task parameters, inherent to this dev CLI's file operations
	if err != nil {
		return nil, fmt.Errorf("read file %s: %w", path, err)
	}

	content := string(data)
	lines := strings.Split(content, "\n")

	cf := &ConflictFile{
		Path:        path,
		FullContent: content,
	}

	i := 0
	for i < len(lines) {
		if strings.HasPrefix(lines[i], "<<<<<<<") {
			conflict, endIdx := parseConflictRegion(lines, i)
			conflict.File = path
			cf.Conflicts = append(cf.Conflicts, conflict)
			i = endIdx + 1
		} else {
			i++
		}
	}

	if len(cf.Conflicts) == 0 {
		return nil, fmt.Errorf("no conflicts found in %s", path)
	}

	return cf, nil
}

// parseConflictRegion extracts a single conflict starting at the given line index.
func parseConflictRegion(lines []string, startIdx int) (Conflict, int) {
	conflict := Conflict{
		StartLine: startIdx + 1, // 1-based
	}

	var oursLines []string
	var baseLines []string
	var theirsLines []string

	// States: 0 = ours, 1 = base, 2 = theirs
	state := 0
	i := startIdx + 1 // Skip the <<<<<<< marker line

	for i < len(lines) {
		line := lines[i]

		if strings.HasPrefix(line, "|||||||") {
			// 3-way merge base marker
			state = 1
			i++
			continue
		}

		if strings.HasPrefix(line, "=======") {
			state = 2
			i++
			continue
		}

		if strings.HasPrefix(line, ">>>>>>>") {
			conflict.EndLine = i + 1 // 1-based
			break
		}

		switch state {
		case 0:
			oursLines = append(oursLines, line)
		case 1:
			baseLines = append(baseLines, line)
		case 2:
			theirsLines = append(theirsLines, line)
		}
		i++
	}

	conflict.OursContent = strings.Join(oursLines, "\n")
	conflict.BaseContent = strings.Join(baseLines, "\n")
	conflict.TheirsContent = strings.Join(theirsLines, "\n")

	return conflict, i
}

// AutoResolve attempts to automatically resolve all conflicts in a ConflictFile.
func (cr *ConflictResolver) AutoResolve(cf *ConflictFile) (string, error) {
	cr.mu.Lock()
	defer cr.mu.Unlock()

	lines := strings.Split(cf.FullContent, "\n")
	var result []string
	i := 0

	conflictIdx := 0
	for i < len(lines) {
		if strings.HasPrefix(lines[i], "<<<<<<<") && conflictIdx < len(cf.Conflicts) {
			conflict := &cf.Conflicts[conflictIdx]

			resolution := cr.resolveConflict(conflict)
			conflict.Resolved = resolution != ""

			if conflict.Resolved {
				conflict.Resolution = resolution
				if resolution != "" {
					result = append(result, strings.Split(resolution, "\n")...)
				}
			} else {
				// Leave markers with a comment for manual resolution
				conflict.Resolution = ""
				result = append(result, "// CONFLICT: Could not auto-resolve - both sides modified same lines")
				result = append(result, lines[i]) // <<<<<<<
				// Re-add the conflict as-is
				for j := i + 1; j < len(lines); j++ {
					result = append(result, lines[j])
					if strings.HasPrefix(lines[j], ">>>>>>>") {
						i = j
						break
					}
				}
			}

			// Skip past the conflict markers
			for i < len(lines) && !strings.HasPrefix(lines[i], ">>>>>>>") {
				i++
			}
			i++ // Skip >>>>>>> line
			conflictIdx++
		} else {
			result = append(result, lines[i])
			i++
		}
	}

	resolved := strings.Join(result, "\n")
	cf.FullContent = resolved
	return resolved, nil
}

// resolveConflict applies resolution strategies to a single conflict.
func (cr *ConflictResolver) resolveConflict(c *Conflict) string {
	// First try trivial resolution
	if resolution, ok := ResolveTrivial(c.OursContent, c.TheirsContent); ok {
		return resolution
	}

	// Strategy-based resolution
	switch cr.Strategy {
	case "ours":
		return c.OursContent
	case "theirs":
		return c.TheirsContent
	case "smart":
		return cr.smartResolve(c)
	default:
		return c.OursContent
	}
}

// smartResolve attempts intelligent resolution based on the nature of the conflict.
func (cr *ConflictResolver) smartResolve(c *Conflict) string {
	// Check for import conflicts
	if isImportBlock(c.OursContent) || isImportBlock(c.TheirsContent) {
		return ResolveImports(c.OursContent, c.TheirsContent)
	}

	// Try additive resolution
	if c.BaseContent != "" {
		additive := ResolveAdditive(c.OursContent, c.TheirsContent, c.BaseContent)
		if additive != "" {
			return additive
		}
	}

	// One side deletes, other modifies: keep the modification
	if strings.TrimSpace(c.OursContent) == "" && strings.TrimSpace(c.TheirsContent) != "" {
		return c.TheirsContent
	}
	if strings.TrimSpace(c.TheirsContent) == "" && strings.TrimSpace(c.OursContent) != "" {
		return c.OursContent
	}

	// If both add different content (no base), combine
	if c.BaseContent == "" && c.OursContent != "" && c.TheirsContent != "" {
		// Check if they're modifying the same lines or adding new ones
		oursLines := strings.Split(c.OursContent, "\n")
		theirsLines := strings.Split(c.TheirsContent, "\n")

		// If both are purely additive (no overlap), combine
		if !hasOverlappingLines(oursLines, theirsLines) {
			return c.OursContent + "\n" + c.TheirsContent
		}
	}

	// Cannot auto-resolve
	return ""
}

// isImportBlock detects if content looks like an import block.
func isImportBlock(content string) bool {
	lines := strings.Split(strings.TrimSpace(content), "\n")
	if len(lines) == 0 {
		return false
	}

	importCount := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "import ") ||
			strings.HasPrefix(trimmed, "\"") ||
			strings.HasPrefix(trimmed, "from ") ||
			strings.HasPrefix(trimmed, "require(") ||
			(strings.HasPrefix(trimmed, "\"") && strings.HasSuffix(trimmed, "\"")) ||
			strings.Contains(trimmed, "import") {
			importCount++
		}
	}

	// Consider it an import block if at least half the lines look like imports
	return importCount > 0 && importCount >= len(lines)/2
}

// hasOverlappingLines checks if two sets of lines have non-trivial overlap.
func hasOverlappingLines(a, b []string) bool {
	set := make(map[string]bool, len(a))
	for _, line := range a {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			set[trimmed] = true
		}
	}
	for _, line := range b {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && set[trimmed] {
			return true
		}
	}
	return false
}

// ResolveImports merges import blocks, deduplicates, and sorts.
func ResolveImports(ours, theirs string) string {
	oursImports := extractImportLines(ours)
	theirsImports := extractImportLines(theirs)

	// Merge and deduplicate
	seen := make(map[string]bool)
	var merged []string

	for _, imp := range oursImports {
		normalized := strings.TrimSpace(imp)
		if normalized != "" && !seen[normalized] {
			seen[normalized] = true
			merged = append(merged, normalized)
		}
	}
	for _, imp := range theirsImports {
		normalized := strings.TrimSpace(imp)
		if normalized != "" && !seen[normalized] {
			seen[normalized] = true
			merged = append(merged, normalized)
		}
	}

	// Sort imports
	sort.Strings(merged)

	return strings.Join(merged, "\n")
}

// extractImportLines splits content into individual import lines.
func extractImportLines(content string) []string {
	lines := strings.Split(content, "\n")
	var imports []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			imports = append(imports, trimmed)
		}
	}
	return imports
}

// ResolveAdditive resolves conflicts where both sides add new lines relative to base.
func ResolveAdditive(ours, theirs, base string) string {
	if base == "" {
		return ""
	}

	baseLines := strings.Split(base, "\n")
	oursLines := strings.Split(ours, "\n")
	theirsLines := strings.Split(theirs, "\n")

	// Determine what each side added relative to base
	oursAdded := diffLines(baseLines, oursLines)
	theirsAdded := diffLines(baseLines, theirsLines)

	// Determine what each side removed
	oursRemoved := diffLines(oursLines, baseLines)
	theirsRemoved := diffLines(theirsLines, baseLines)

	// If both sides only add lines (no modifications to existing), combine
	if len(oursRemoved) == 0 && len(theirsRemoved) == 0 {
		// Start with base, add both sets of additions
		result := make([]string, len(baseLines))
		copy(result, baseLines)

		// Append ours additions
		result = append(result, oursAdded...)
		// Append theirs additions (avoiding duplicates)
		seen := make(map[string]bool)
		for _, line := range result {
			seen[strings.TrimSpace(line)] = true
		}
		for _, line := range theirsAdded {
			if !seen[strings.TrimSpace(line)] {
				result = append(result, line)
			}
		}

		return strings.Join(result, "\n")
	}

	// If one side only deletes and the other only modifies, keep modification
	if len(oursAdded) == 0 && len(oursRemoved) > 0 && len(theirsAdded) > 0 {
		return theirs
	}
	if len(theirsAdded) == 0 && len(theirsRemoved) > 0 && len(oursAdded) > 0 {
		return ours
	}

	return ""
}

// diffLines returns lines present in b but not in a.
func diffLines(a, b []string) []string {
	aSet := make(map[string]bool, len(a))
	for _, line := range a {
		aSet[strings.TrimSpace(line)] = true
	}

	var diff []string
	for _, line := range b {
		if !aSet[strings.TrimSpace(line)] {
			diff = append(diff, line)
		}
	}
	return diff
}

// ResolveTrivial handles trivial conflict cases.
// Returns the resolution and true if resolved, empty string and false otherwise.
func ResolveTrivial(ours, theirs string) (string, bool) {
	// Identical content: deduplicate
	if ours == theirs {
		return ours, true
	}

	// One side is empty: take the other
	if strings.TrimSpace(ours) == "" {
		return theirs, true
	}
	if strings.TrimSpace(theirs) == "" {
		return ours, true
	}

	// Whitespace-only difference: take ours (arbitrary choice)
	if strings.TrimSpace(ours) == strings.TrimSpace(theirs) {
		return ours, true
	}

	return "", false
}

// ApplyResolution writes the resolved content back to the file.
func (cr *ConflictResolver) ApplyResolution(cf *ConflictFile) error {
	cr.mu.Lock()
	defer cr.mu.Unlock()

	if cf.FullContent == "" {
		return fmt.Errorf("no resolved content to write")
	}

	err := os.WriteFile(cf.Path, []byte(cf.FullContent), 0o600)
	if err != nil {
		return fmt.Errorf("write resolved file %s: %w", cf.Path, err)
	}

	return nil
}

// FormatConflicts produces a human-readable summary of conflicts and their resolution status.
func FormatConflicts(cf *ConflictFile) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("Conflicts in %s:\n", cf.Path))
	b.WriteString("───────────────────────────────\n")

	autoResolved := 0
	needsReview := 0

	for i, c := range cf.Conflicts {
		conflictType := classifyConflict(&c)
		b.WriteString(fmt.Sprintf("\nConflict %d (L%d-L%d): %s\n", i+1, c.StartLine, c.EndLine, conflictType))

		if c.Resolved {
			autoResolved++
			b.WriteString(fmt.Sprintf("  Resolution: AUTO (%s)\n", describeResolution(&c)))
		} else {
			needsReview++
			b.WriteString("  Resolution: MANUAL NEEDED (both sides modified same lines)\n")
		}
	}

	b.WriteString(fmt.Sprintf("\nSummary: %d auto-resolved, %d needs review\n", autoResolved, needsReview))

	return b.String()
}

// classifyConflict determines the type of a conflict based on its content.
func classifyConflict(c *Conflict) string {
	if isImportBlock(c.OursContent) || isImportBlock(c.TheirsContent) {
		return "Import block"
	}

	oursLines := len(strings.Split(c.OursContent, "\n"))
	theirsLines := len(strings.Split(c.TheirsContent, "\n"))

	if oursLines == 1 && theirsLines == 1 {
		return "Single line"
	}

	if strings.TrimSpace(c.OursContent) == "" || strings.TrimSpace(c.TheirsContent) == "" {
		return "Deletion vs modification"
	}

	return "Function body"
}

// describeResolution provides a short description of how a conflict was resolved.
func describeResolution(c *Conflict) string {
	if isImportBlock(c.OursContent) || isImportBlock(c.TheirsContent) {
		return "merged imports"
	}

	if c.OursContent == c.TheirsContent {
		return "identical content deduplicated"
	}

	if strings.TrimSpace(c.OursContent) == strings.TrimSpace(c.TheirsContent) {
		return "whitespace difference resolved"
	}

	if strings.TrimSpace(c.OursContent) == "" {
		return "kept theirs (ours empty)"
	}
	if strings.TrimSpace(c.TheirsContent) == "" {
		return "kept ours (theirs empty)"
	}

	return "smart merge"
}

// --- ConflictResolverTool implements the Tool interface ---

// ConflictResolverTool resolves git merge conflicts in files.
type ConflictResolverTool struct{}

// ConflictResolverInput is the typed input for ConflictResolverTool.
type ConflictResolverInput struct {
	Path     string `json:"path"`
	Strategy string `json:"strategy"`
	DryRun   bool   `json:"dry_run"`
}

func (ConflictResolverTool) Name() string { return "ResolveConflicts" }

func (ConflictResolverTool) Description() string {
	return "Automatically resolve git merge conflicts in files. Supports smart resolution strategies including import merging, additive merging, and trivial conflict resolution."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (ConflictResolverTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"path":     {Type: "string", Description: "Absolute path to the file with merge conflicts"},
			"strategy": {Type: "string", Enum: []interface{}{"smart", "ours", "theirs"}, Description: "Resolution strategy: 'smart' (default), 'ours', or 'theirs'"},
			"dry_run":  {Type: "boolean", Description: "If true, show resolution plan without modifying the file"},
		},
		Required: []string{"path"},
	}
}

func (ConflictResolverTool) Parameters() map[string]interface{} {
	return conflictResolverSchema.ToJSONSchema()
}

// conflictResolverSchema is the single source of truth for ConflictResolver's input schema.
var conflictResolverSchema = ConflictResolverTool{}.Schema()

func (ConflictResolverTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[ConflictResolverInput]("ConflictResolver", input)
	if err != nil {
		return "", err
	}
	if p.Path == "" {
		return "", fmt.Errorf("path is required")
	}
	if p.Strategy == "" {
		p.Strategy = "smart"
	}

	resolver := NewConflictResolver()
	resolver.Strategy = p.Strategy

	cf, err := resolver.ParseConflicts(p.Path)
	if err != nil {
		return "", err
	}

	_, err = resolver.AutoResolve(cf)
	if err != nil {
		return "", fmt.Errorf("auto-resolve: %w", err)
	}

	summary := FormatConflicts(cf)

	if p.DryRun {
		return summary, nil
	}

	if err := resolver.ApplyResolution(cf); err != nil {
		return "", err
	}

	return summary + "\nResolved content written to " + p.Path, nil
}

// Ensure ConflictResolverTool satisfies Tool interface at compile time.
var _ Tool = ConflictResolverTool{}
