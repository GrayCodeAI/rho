package fingerprint

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// Fingerprint holds a structured summary of a repository or directory.
type Fingerprint struct {
	Name           string         // repo/directory name
	Languages      []LanguageInfo // detected languages with percentages
	TotalFiles     int
	TotalLines     int
	Dependencies   int // count from package manager files
	HasTests       bool
	HasCI          bool
	License        string
	GitInfo        *GitInfo
	PackageManager string // npm, go mod, cargo, pip, etc.
}

// LanguageInfo holds detection results for a single programming language.
type LanguageInfo struct {
	Name       string
	Percentage float64
	Files      int
	Lines      int
}

// GitInfo holds version-control metadata extracted via git commands.
type GitInfo struct {
	Branch       string
	CommitCount  int
	LastCommit   string
	Contributors int
}

// Generate produces a fingerprint for the given directory.
func Generate(dir string) (*Fingerprint, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("fingerprint: resolve path: %w", err)
	}

	fp := &Fingerprint{
		Name: filepath.Base(absDir),
	}

	// Walk directory to collect file stats.
	stats, totalLines, err := walkDir(absDir)
	if err != nil {
		return nil, fmt.Errorf("fingerprint: walk directory: %w", err)
	}

	fp.TotalFiles = 0
	fp.TotalLines = totalLines
	for _, s := range stats {
		fp.TotalFiles += s.files
	}

	// Build language info sorted by line count descending.
	fp.Languages = buildLanguageInfo(stats, totalLines)

	// Detect package manager and count dependencies.
	fp.PackageManager, fp.Dependencies = detectPackageManager(absDir)

	// Detect tests.
	fp.HasTests = detectTests(absDir)

	// Detect CI.
	fp.HasCI = detectCI(absDir)

	// Detect license.
	fp.License = detectLicense(absDir)

	// Collect git info (best-effort).
	fp.GitInfo = collectGitInfo(absDir)

	return fp, nil
}

// buildLanguageInfo converts raw stats into sorted LanguageInfo with percentages.
func buildLanguageInfo(stats map[string]*langStat, totalLines int) []LanguageInfo {
	if totalLines == 0 {
		return nil
	}

	langs := make([]LanguageInfo, 0, len(stats))
	for name, s := range stats {
		pct := float64(s.lines) / float64(totalLines) * 100
		langs = append(langs, LanguageInfo{
			Name:       name,
			Percentage: pct,
			Files:      s.files,
			Lines:      s.lines,
		})
	}

	sort.Slice(langs, func(i, j int) bool {
		return langs[i].Lines > langs[j].Lines
	})

	return langs
}

// Format renders the fingerprint as a concise string suitable for LLM context
// injection. The output targets under 500 tokens.
func (f *Fingerprint) Format() string {
	var b strings.Builder

	b.WriteString("Repo: ")
	b.WriteString(f.Name)
	b.WriteByte('\n')

	// Languages (top 5).
	if len(f.Languages) > 0 {
		b.WriteString("Languages: ")
		limit := len(f.Languages)
		if limit > 5 {
			limit = 5
		}
		parts := make([]string, limit)
		for i := 0; i < limit; i++ {
			l := f.Languages[i]
			parts[i] = fmt.Sprintf("%s %.1f%%", l.Name, l.Percentage)
		}
		b.WriteString(strings.Join(parts, ", "))
		b.WriteByte('\n')
	}

	b.WriteString(fmt.Sprintf("Files: %d | Lines: %d\n", f.TotalFiles, f.TotalLines))

	if f.PackageManager != "" {
		b.WriteString(fmt.Sprintf("Package manager: %s (%d deps)\n", f.PackageManager, f.Dependencies))
	}

	flags := make([]string, 0, 3)
	if f.HasTests {
		flags = append(flags, "tests")
	}
	if f.HasCI {
		flags = append(flags, "CI")
	}
	if f.License != "" {
		flags = append(flags, "license:"+f.License)
	}
	if len(flags) > 0 {
		b.WriteString("Features: ")
		b.WriteString(strings.Join(flags, ", "))
		b.WriteByte('\n')
	}

	if f.GitInfo != nil {
		b.WriteString(fmt.Sprintf("Git: branch=%s commits=%d contributors=%d\n",
			f.GitInfo.Branch, f.GitInfo.CommitCount, f.GitInfo.Contributors))
		if f.GitInfo.LastCommit != "" {
			b.WriteString("Last commit: ")
			b.WriteString(f.GitInfo.LastCommit)
			b.WriteByte('\n')
		}
	}

	return b.String()
}

// FormatMarkdown renders the fingerprint as markdown for display.
func (f *Fingerprint) FormatMarkdown() string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("# %s\n\n", f.Name))

	// Summary table.
	b.WriteString("| Metric | Value |\n")
	b.WriteString("|--------|-------|\n")
	b.WriteString(fmt.Sprintf("| Files | %d |\n", f.TotalFiles))
	b.WriteString(fmt.Sprintf("| Lines | %d |\n", f.TotalLines))
	if f.PackageManager != "" {
		b.WriteString(fmt.Sprintf("| Package Manager | %s |\n", f.PackageManager))
		b.WriteString(fmt.Sprintf("| Dependencies | %d |\n", f.Dependencies))
	}
	b.WriteString(fmt.Sprintf("| Tests | %v |\n", f.HasTests))
	b.WriteString(fmt.Sprintf("| CI | %v |\n", f.HasCI))
	if f.License != "" {
		b.WriteString(fmt.Sprintf("| License | %s |\n", f.License))
	}
	b.WriteByte('\n')

	// Languages.
	if len(f.Languages) > 0 {
		b.WriteString("## Languages\n\n")
		b.WriteString("| Language | % | Files | Lines |\n")
		b.WriteString("|----------|---|-------|-------|\n")
		limit := len(f.Languages)
		if limit > 10 {
			limit = 10
		}
		for i := 0; i < limit; i++ {
			l := f.Languages[i]
			b.WriteString(fmt.Sprintf("| %s | %.1f%% | %d | %d |\n",
				l.Name, l.Percentage, l.Files, l.Lines))
		}
		b.WriteByte('\n')
	}

	// Git info.
	if f.GitInfo != nil {
		b.WriteString("## Git\n\n")
		b.WriteString(fmt.Sprintf("- **Branch:** %s\n", f.GitInfo.Branch))
		b.WriteString(fmt.Sprintf("- **Commits:** %d\n", f.GitInfo.CommitCount))
		b.WriteString(fmt.Sprintf("- **Contributors:** %d\n", f.GitInfo.Contributors))
		if f.GitInfo.LastCommit != "" {
			b.WriteString(fmt.Sprintf("- **Last commit:** %s\n", f.GitInfo.LastCommit))
		}
		b.WriteByte('\n')
	}

	return b.String()
}
