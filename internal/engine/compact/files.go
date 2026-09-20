package compact

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/GrayCodeAI/rho/internal/permissions"
	"github.com/GrayCodeAI/rho/internal/types"
)

type FileTracker struct {
	ReadFiles     map[string]int
	ModifiedFiles map[string]int
}

func NewFileTracker() *FileTracker {
	return &FileTracker{
		ReadFiles:     make(map[string]int),
		ModifiedFiles: make(map[string]int),
	}
}

func (ft *FileTracker) RecordRead(path string) {
	if path == "" {
		return
	}
	ft.ReadFiles[path]++
}

func (ft *FileTracker) RecordModified(path string) {
	if path == "" {
		return
	}
	ft.ModifiedFiles[path]++
}

func (ft *FileTracker) ExtractFromMessages(messages []types.FluxMessage) {
	for _, msg := range messages {
		if msg.Role != "assistant" {
			continue
		}
		for _, tc := range msg.ToolUse {
			cn := canonicalToolName(tc.Name)
			p, ok := pathArgument(tc.Arguments)
			if !ok {
				continue
			}
			switch cn {
			case "Read":
				ft.ReadFiles[p]++
			case "Write", "Edit":
				ft.ModifiedFiles[p]++
			}
		}
	}
}

func (ft *FileTracker) FormatForSummary() string {
	if len(ft.ReadFiles) == 0 && len(ft.ModifiedFiles) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("<tracked-files>\n")

	if len(ft.ReadFiles) > 0 {
		sb.WriteString("Read: ")
		sb.WriteString(formatPathCounts(ft.ReadFiles))
		sb.WriteString("\n")
	}

	if len(ft.ModifiedFiles) > 0 {
		sb.WriteString("Modified: ")
		sb.WriteString(formatPathCounts(ft.ModifiedFiles))
		sb.WriteString("\n")
	}

	sb.WriteString("</tracked-files>")
	return sb.String()
}

func (ft *FileTracker) ParseFromSummary(summary string) {
	start := strings.Index(summary, "<tracked-files>")
	end := strings.Index(summary, "</tracked-files>")
	if start < 0 || end <= start {
		return
	}

	block := summary[start+len("<tracked-files>") : end]
	lines := strings.Split(strings.TrimSpace(block), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Read: ") {
			parsePathLine(line[len("Read: "):], ft.ReadFiles)
		} else if strings.HasPrefix(line, "Modified: ") {
			parsePathLine(line[len("Modified: "):], ft.ModifiedFiles)
		}
	}
}

func (ft *FileTracker) Merge(other *FileTracker) {
	if other == nil {
		return
	}
	for path, count := range other.ReadFiles {
		ft.ReadFiles[path] += count
	}
	for path, count := range other.ModifiedFiles {
		ft.ModifiedFiles[path] += count
	}
}

func formatPathCounts(m map[string]int) string {
	paths := make([]string, 0, len(m))
	for p := range m {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	parts := make([]string, 0, len(paths))
	for _, p := range paths {
		parts = append(parts, fmt.Sprintf("%s (%dx)", p, m[p]))
	}
	return strings.Join(parts, ", ")
}

func parsePathLine(line string, target map[string]int) {
	entries := strings.Split(line, ", ")
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		parenIdx := strings.LastIndex(entry, " (")
		if parenIdx < 0 {
			target[entry]++
			continue
		}

		path := entry[:parenIdx]
		countStr := entry[parenIdx+2:]
		countStr = strings.TrimSuffix(countStr, ")")
		countStr = strings.TrimSuffix(countStr, "x")

		count, err := strconv.Atoi(countStr)
		if err != nil {
			count = 1
		}
		target[path] += count
	}
}

func canonicalToolName(name string) string {
	return permissions.CanonicalToolName(name)
}

func pathArgument(args map[string]interface{}) (string, bool) {
	if p, ok := args["path"].(string); ok && p != "" {
		return p, true
	}
	if p, ok := args["file_path"].(string); ok && p != "" {
		return p, true
	}
	return "", false
}
