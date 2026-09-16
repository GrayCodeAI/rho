package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PatchParser holds parsed file patches from a structured patch input.
type PatchParser struct {
	patches []FilePatch
}

// FilePatch represents modifications to a single file.
type FilePatch struct {
	Path     string
	Hunks    []Hunk
	IsNew    bool // true if creating a new file
	IsDelete bool // true if deleting file
}

// Hunk represents a single change within a file, anchored by context lines.
type Hunk struct {
	ContextBefore string   // line(s) before the change for anchoring
	ContextAfter  string   // line(s) after the change for anchoring
	OldLines      []string // lines to remove (without the leading "- ")
	NewLines      []string // lines to add (without the leading "+ ")
}

// ParsePatch parses a structured patch in the *** Begin Patch format.
func ParsePatch(input string) (*PatchParser, error) {
	lines := strings.Split(input, "\n")
	parser := &PatchParser{}

	i := 0
	// Find the beginning of the patch
	for i < len(lines) {
		if strings.TrimSpace(lines[i]) == "*** Begin Patch" {
			i++
			break
		}
		i++
	}
	if i >= len(lines) {
		return nil, fmt.Errorf("patch missing '*** Begin Patch' marker")
	}

	for i < len(lines) {
		line := strings.TrimSpace(lines[i])
		if line == "*** End Patch" {
			break
		}

		var fp FilePatch
		switch {
		case strings.HasPrefix(line, "*** Update File:"):
			fp.Path = strings.TrimSpace(strings.TrimPrefix(line, "*** Update File:"))
			i++
		case strings.HasPrefix(line, "*** Create File:"):
			fp.Path = strings.TrimSpace(strings.TrimPrefix(line, "*** Create File:"))
			fp.IsNew = true
			i++
		case strings.HasPrefix(line, "*** Delete File:"):
			fp.Path = strings.TrimSpace(strings.TrimPrefix(line, "*** Delete File:"))
			fp.IsDelete = true
			i++
			parser.patches = append(parser.patches, fp)
			continue
		default:
			return nil, fmt.Errorf("unexpected line at position %d: %q", i, lines[i])
		}

		// Parse hunks for this file
		for i < len(lines) {
			trimmed := strings.TrimSpace(lines[i])
			if trimmed == "*** End Patch" ||
				strings.HasPrefix(trimmed, "*** Update File:") ||
				strings.HasPrefix(trimmed, "*** Create File:") ||
				strings.HasPrefix(trimmed, "*** Delete File:") {
				break
			}

			// Expect a context line marker: @@@ ... @@@
			if strings.HasPrefix(trimmed, "@@@") && strings.HasSuffix(trimmed, "@@@") {
				hunk := Hunk{}
				// Extract context before
				contextBefore := strings.TrimSpace(trimmed[3 : len(trimmed)-3])
				hunk.ContextBefore = contextBefore
				i++

				// Parse old/new lines until we hit another @@@ or end
				for i < len(lines) {
					l := lines[i]
					lt := strings.TrimSpace(l)
					if strings.HasPrefix(lt, "@@@") && strings.HasSuffix(lt, "@@@") {
						// This is the context after marker
						contextAfter := strings.TrimSpace(lt[3 : len(lt)-3])
						hunk.ContextAfter = contextAfter
						i++
						break
					}
					if lt == "*** End Patch" ||
						strings.HasPrefix(lt, "*** Update File:") ||
						strings.HasPrefix(lt, "*** Create File:") ||
						strings.HasPrefix(lt, "*** Delete File:") {
						break
					}

					if strings.HasPrefix(l, "- ") {
						hunk.OldLines = append(hunk.OldLines, l[2:])
					} else if strings.HasPrefix(l, "+ ") {
						hunk.NewLines = append(hunk.NewLines, l[2:])
					} else if l == "-" {
						// empty old line (just a dash with no trailing content)
						hunk.OldLines = append(hunk.OldLines, "")
					} else if l == "+" {
						// empty new line (just a plus with no trailing content)
						hunk.NewLines = append(hunk.NewLines, "")
					} else {
						// Unrecognized line inside a hunk; skip or treat as error
						return nil, fmt.Errorf("unexpected line in hunk at position %d: %q", i, l)
					}
					i++
				}

				fp.Hunks = append(fp.Hunks, hunk)
			} else {
				// For new files, lines without prefix are content lines
				if fp.IsNew {
					hunk := Hunk{}
					for i < len(lines) {
						l := lines[i]
						lt := strings.TrimSpace(l)
						if lt == "*** End Patch" ||
							strings.HasPrefix(lt, "*** Update File:") ||
							strings.HasPrefix(lt, "*** Create File:") ||
							strings.HasPrefix(lt, "*** Delete File:") {
							break
						}
						if strings.HasPrefix(l, "+ ") {
							hunk.NewLines = append(hunk.NewLines, l[2:])
						} else if l == "+" {
							hunk.NewLines = append(hunk.NewLines, "")
						} else {
							// Treat unrecognized line as content for new file
							hunk.NewLines = append(hunk.NewLines, l)
						}
						i++
					}
					if len(hunk.NewLines) > 0 {
						fp.Hunks = append(fp.Hunks, hunk)
					}
				} else {
					return nil, fmt.Errorf("expected @@@ context marker at position %d, got: %q", i, lines[i])
				}
			}
		}

		parser.patches = append(parser.patches, fp)
	}

	if len(parser.patches) == 0 {
		return nil, fmt.Errorf("no file patches found in input")
	}

	return parser, nil
}

// Patches returns the parsed file patches.
func (p *PatchParser) Patches() []FilePatch {
	return p.patches
}

// Apply applies a single FilePatch to disk.
func Apply(patch *FilePatch) error {
	if patch.IsDelete {
		if _, err := os.Stat(patch.Path); os.IsNotExist(err) {
			return fmt.Errorf("cannot delete non-existent file: %s", patch.Path)
		}
		return os.Remove(patch.Path)
	}

	if patch.IsNew {
		// Ensure parent directory exists
		dir := filepath.Dir(patch.Path)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
		var content []string
		for _, h := range patch.Hunks {
			content = append(content, h.NewLines...)
		}
		return os.WriteFile(patch.Path, []byte(strings.Join(content, "\n")+"\n"), 0o600)
	}

	// Update existing file
	data, err := os.ReadFile(patch.Path)
	if err != nil {
		return fmt.Errorf("cannot read file %s: %w", patch.Path, err)
	}

	lines := strings.Split(string(data), "\n")

	// Apply hunks in reverse order to preserve line numbers
	type resolvedHunk struct {
		startLine int
		endLine   int
		hunk      Hunk
	}
	var resolved []resolvedHunk

	for _, h := range patch.Hunks {
		start, end, err := findHunkLocation(lines, h)
		if err != nil {
			return fmt.Errorf("in file %s: %w", patch.Path, err)
		}
		resolved = append(resolved, resolvedHunk{startLine: start, endLine: end, hunk: h})
	}

	// Sort in reverse order by start line to avoid offset issues
	for i := 0; i < len(resolved)-1; i++ {
		for j := i + 1; j < len(resolved); j++ {
			if resolved[j].startLine > resolved[i].startLine {
				resolved[i], resolved[j] = resolved[j], resolved[i]
			}
		}
	}

	for _, rh := range resolved {
		// Replace old lines with new lines
		var newLines []string
		newLines = append(newLines, lines[:rh.startLine]...)
		newLines = append(newLines, rh.hunk.NewLines...)
		newLines = append(newLines, lines[rh.endLine:]...)
		lines = newLines
	}

	result := strings.Join(lines, "\n")
	return writePinnedFile(patch.Path, []byte(result), 0o600)
}

// ApplyAll applies all patches and returns the list of modified file paths.
func (p *PatchParser) ApplyAll(ctx context.Context) ([]string, error) {
	var modified []string
	for i := range p.patches {
		if err := validatePathAllowed(ctx, p.patches[i].Path); err != nil {
			return modified, err
		}
		if err := Apply(&p.patches[i]); err != nil {
			return modified, fmt.Errorf("failed to apply patch for %s: %w", p.patches[i].Path, err)
		}
		modified = append(modified, p.patches[i].Path)
	}
	return modified, nil
}

// findHunkLocation locates where a hunk should be applied within the file lines.
// It returns the start (inclusive) and end (exclusive) line indices of the old lines.
func findHunkLocation(lines []string, hunk Hunk) (startLine, endLine int, err error) {
	// If we have no context and no old lines, this is an append operation
	if hunk.ContextBefore == "" && hunk.ContextAfter == "" && len(hunk.OldLines) == 0 {
		return len(lines), len(lines), nil
	}

	const maxDistance = 3 // max Levenshtein distance for fuzzy match

	type candidate struct {
		start int
		end   int
		score int // lower is better (sum of distances)
	}
	var candidates []candidate

	for i := 0; i <= len(lines)-len(hunk.OldLines); i++ {
		score := 0

		// Check context before
		if hunk.ContextBefore != "" {
			if i == 0 {
				// No preceding line to check context
				continue
			}
			dist := levenshteinDistance(
				strings.TrimSpace(lines[i-1]),
				strings.TrimSpace(hunk.ContextBefore),
			)
			if dist > maxDistance {
				continue
			}
			score += dist
		}

		// Check old lines match
		if len(hunk.OldLines) > 0 {
			if i+len(hunk.OldLines) > len(lines) {
				continue
			}
			oldMatch := true
			for j, oldLine := range hunk.OldLines {
				dist := levenshteinDistance(
					strings.TrimSpace(lines[i+j]),
					strings.TrimSpace(oldLine),
				)
				if dist > maxDistance {
					oldMatch = false
					break
				}
				score += dist
			}
			if !oldMatch {
				continue
			}
		}

		end := i + len(hunk.OldLines)

		// Check context after
		if hunk.ContextAfter != "" {
			if end >= len(lines) {
				continue
			}
			dist := levenshteinDistance(
				strings.TrimSpace(lines[end]),
				strings.TrimSpace(hunk.ContextAfter),
			)
			if dist > maxDistance {
				continue
			}
			score += dist
		}

		candidates = append(candidates, candidate{start: i, end: end, score: score})
	}

	if len(candidates) == 0 {
		return 0, 0, fmt.Errorf("could not find location for hunk (context_before=%q, old_lines=%d)", hunk.ContextBefore, len(hunk.OldLines))
	}

	// Find best match
	best := candidates[0]
	ambiguous := false
	for _, c := range candidates[1:] {
		if c.score < best.score {
			best = c
			ambiguous = false
		} else if c.score == best.score {
			ambiguous = true
		}
	}

	if ambiguous {
		return 0, 0, fmt.Errorf("ambiguous context match: multiple locations match equally well (context_before=%q)", hunk.ContextBefore)
	}

	return best.start, best.end, nil
}

// levenshteinDistance calculates the Levenshtein edit distance between two strings.
func levenshteinDistance(a, b string) int {
	if a == b {
		return 0
	}
	la := len(a)
	lb := len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}

	// Use two rows for space efficiency
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)

	for j := 0; j <= lb; j++ {
		prev[j] = j
	}

	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			ins := curr[j-1] + 1
			del := prev[j] + 1
			sub := prev[j-1] + cost
			min := ins
			if del < min {
				min = del
			}
			if sub < min {
				min = sub
			}
			curr[j] = min
		}
		prev, curr = curr, prev
	}

	return prev[lb]
}

// PatchTool implements the Tool interface for applying structured patches.
type PatchTool struct{}

// PatchInput is the typed input for PatchTool.
type PatchInput struct {
	Patch string `json:"patch"`
}

func (PatchTool) Name() string { return "Patch" }

func (PatchTool) Description() string {
	return "Apply a structured patch to one or more files. Supports context-anchored hunks for precise modifications."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (PatchTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"patch": {Type: "string", Description: "Patch content in the *** Begin Patch format"},
		},
		Required: []string{"patch"},
	}
}

func (PatchTool) Parameters() map[string]interface{} {
	return patchSchema.ToJSONSchema()
}

// patchSchema is the single source of truth for Patch's input schema.
var patchSchema = PatchTool{}.Schema()

func (PatchTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	params, err := DecodeInput[PatchInput]("Patch", input)
	if err != nil {
		return "", err
	}
	if params.Patch == "" {
		return "", fmt.Errorf("patch content is required")
	}

	parser, err := ParsePatch(params.Patch)
	if err != nil {
		return "", fmt.Errorf("failed to parse patch: %w", err)
	}

	// Reject any patch whose target path escapes the workspace before
	// applying — otherwise an LLM-authored patch could write/delete files
	// outside the sandbox (validated below per-entry).
	for _, fp := range parser.Patches() {
		if vErr := validatePathAllowed(ctx, fp.Path); vErr != nil {
			return "", vErr
		}
	}

	modified, err := parser.ApplyAll(ctx)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("Successfully applied patch to %d file(s): %s", len(modified), strings.Join(modified, ", ")), nil
}
