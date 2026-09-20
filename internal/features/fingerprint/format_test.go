package fingerprint

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// buildLanguageInfo
// ---------------------------------------------------------------------------

func TestBuildLanguageInfo_Empty(t *testing.T) {
	langs := buildLanguageInfo(nil, 0)
	if len(langs) != 0 {
		t.Errorf("expected 0 languages, got %d", len(langs))
	}
}

func TestBuildLanguageInfo_ZeroTotalLines(t *testing.T) {
	stats := map[string]*langStat{
		"Go": {files: 5, lines: 0},
	}
	langs := buildLanguageInfo(stats, 0)
	if len(langs) != 0 {
		t.Errorf("expected 0 languages when totalLines=0, got %d", len(langs))
	}
}

func TestBuildLanguageInfo_SingleLanguage(t *testing.T) {
	stats := map[string]*langStat{
		"Go": {files: 10, lines: 1000},
	}
	langs := buildLanguageInfo(stats, 1000)
	if len(langs) != 1 {
		t.Fatalf("expected 1 language, got %d", len(langs))
	}
	if langs[0].Name != "Go" {
		t.Errorf("name = %q, want 'Go'", langs[0].Name)
	}
	if langs[0].Percentage != 100.0 {
		t.Errorf("percentage = %f, want 100.0", langs[0].Percentage)
	}
	if langs[0].Files != 10 {
		t.Errorf("files = %d, want 10", langs[0].Files)
	}
	if langs[0].Lines != 1000 {
		t.Errorf("lines = %d, want 1000", langs[0].Lines)
	}
}

func TestBuildLanguageInfo_MultipleSortedByLines(t *testing.T) {
	stats := map[string]*langStat{
		"Python":     {files: 3, lines: 200},
		"Go":         {files: 10, lines: 1000},
		"JavaScript": {files: 5, lines: 500},
	}
	langs := buildLanguageInfo(stats, 1700)
	if len(langs) != 3 {
		t.Fatalf("expected 3 languages, got %d", len(langs))
	}
	// Should be sorted by lines descending: Go, JavaScript, Python
	if langs[0].Name != "Go" {
		t.Errorf("expected Go first, got %q", langs[0].Name)
	}
	if langs[1].Name != "JavaScript" {
		t.Errorf("expected JavaScript second, got %q", langs[1].Name)
	}
	if langs[2].Name != "Python" {
		t.Errorf("expected Python third, got %q", langs[2].Name)
	}
}

func TestBuildLanguageInfo_PercentagesSumTo100(t *testing.T) {
	stats := map[string]*langStat{
		"Go":     {files: 5, lines: 300},
		"Python": {files: 3, lines: 200},
	}
	langs := buildLanguageInfo(stats, 500)
	total := 0.0
	for _, l := range langs {
		total += l.Percentage
	}
	if total < 99.9 || total > 100.1 {
		t.Errorf("percentages sum to %f, expected ~100", total)
	}
}

// ---------------------------------------------------------------------------
// Fingerprint.Format
// ---------------------------------------------------------------------------

func TestFormat_BasicFields(t *testing.T) {
	fp := &Fingerprint{
		Name:       "myproject",
		TotalFiles: 42,
		TotalLines: 5000,
		Languages: []LanguageInfo{
			{Name: "Go", Percentage: 70.0},
			{Name: "Python", Percentage: 30.0},
		},
		PackageManager: "go mod",
		Dependencies:   15,
		HasTests:       true,
		HasCI:          true,
		License:        "MIT",
	}

	out := fp.Format()
	if !strings.Contains(out, "myproject") {
		t.Error("expected repo name in output")
	}
	if !strings.Contains(out, "Go 70.0%") {
		t.Error("expected Go language info in output")
	}
	if !strings.Contains(out, "42") {
		t.Error("expected file count in output")
	}
	if !strings.Contains(out, "5000") {
		t.Error("expected line count in output")
	}
	if !strings.Contains(out, "go mod") {
		t.Error("expected package manager in output")
	}
	if !strings.Contains(out, "15 deps") {
		t.Error("expected dependency count in output")
	}
	if !strings.Contains(out, "tests") {
		t.Error("expected 'tests' flag in output")
	}
	if !strings.Contains(out, "CI") {
		t.Error("expected 'CI' flag in output")
	}
	if !strings.Contains(out, "MIT") {
		t.Error("expected license in output")
	}
}

func TestFormat_NoLanguages(t *testing.T) {
	fp := &Fingerprint{
		Name:       "empty",
		TotalFiles: 0,
		TotalLines: 0,
	}
	out := fp.Format()
	if !strings.Contains(out, "empty") {
		t.Error("expected repo name")
	}
	if strings.Contains(out, "Languages:") {
		t.Error("should not have Languages line when empty")
	}
}

func TestFormat_LimitsTop5Languages(t *testing.T) {
	langs := make([]LanguageInfo, 8)
	for i := range langs {
		langs[i] = LanguageInfo{
			Name:       "Lang" + string(rune('A'+i)),
			Percentage: float64(8 - i),
			Lines:      1000 - i*100,
		}
	}
	fp := &Fingerprint{
		Name:       "multilang",
		TotalFiles: 100,
		TotalLines: 10000,
		Languages:  langs,
	}
	out := fp.Format()
	// Only top 5 should appear in the languages line
	if strings.Contains(out, "LangF") {
		t.Error("6th language should not appear in Format output")
	}
}

func TestFormat_WithGitInfo(t *testing.T) {
	fp := &Fingerprint{
		Name:       "gitted",
		TotalFiles: 50,
		TotalLines: 3000,
		GitInfo: &GitInfo{
			Branch:       "main",
			CommitCount:  100,
			Contributors: 5,
			LastCommit:   "feat: add feature",
		},
	}
	out := fp.Format()
	if !strings.Contains(out, "branch=main") {
		t.Error("expected branch in output")
	}
	if !strings.Contains(out, "commits=100") {
		t.Error("expected commit count in output")
	}
	if !strings.Contains(out, "contributors=5") {
		t.Error("expected contributor count in output")
	}
	if !strings.Contains(out, "feat: add feature") {
		t.Error("expected last commit message in output")
	}
}

func TestFormat_WithoutOptionalFields(t *testing.T) {
	fp := &Fingerprint{
		Name:       "minimal",
		TotalFiles: 1,
		TotalLines: 10,
	}
	out := fp.Format()
	if !strings.Contains(out, "minimal") {
		t.Error("expected repo name")
	}
	if strings.Contains(out, "Package manager:") {
		t.Error("should not show package manager when empty")
	}
	if strings.Contains(out, "Features:") {
		t.Error("should not show features when none")
	}
	if strings.Contains(out, "Git:") {
		t.Error("should not show git info when nil")
	}
}

// ---------------------------------------------------------------------------
// Fingerprint.FormatMarkdown
// ---------------------------------------------------------------------------

func TestFormatMarkdown_ContainsTable(t *testing.T) {
	fp := &Fingerprint{
		Name:       "myproject",
		TotalFiles: 42,
		TotalLines: 5000,
		Languages: []LanguageInfo{
			{Name: "Go", Percentage: 70.0, Files: 10, Lines: 3500},
			{Name: "Python", Percentage: 30.0, Files: 5, Lines: 1500},
		},
		PackageManager: "go mod",
		Dependencies:   15,
		HasTests:       true,
		HasCI:          false,
		License:        "MIT",
	}

	out := fp.FormatMarkdown()
	if !strings.Contains(out, "# myproject") {
		t.Error("expected H1 header with repo name")
	}
	if !strings.Contains(out, "| Metric | Value |") {
		t.Error("expected summary table header")
	}
	if !strings.Contains(out, "| Files | 42 |") {
		t.Error("expected file count in table")
	}
	if !strings.Contains(out, "| Lines | 5000 |") {
		t.Error("expected line count in table")
	}
	if !strings.Contains(out, "| Package Manager | go mod |") {
		t.Error("expected package manager in table")
	}
	if !strings.Contains(out, "| Dependencies | 15 |") {
		t.Error("expected dependencies in table")
	}
	if !strings.Contains(out, "| License | MIT |") {
		t.Error("expected license in table")
	}
}

func TestFormatMarkdown_LanguagesTable(t *testing.T) {
	fp := &Fingerprint{
		Name:       "myproject",
		TotalFiles: 42,
		TotalLines: 5000,
		Languages: []LanguageInfo{
			{Name: "Go", Percentage: 70.0, Files: 10, Lines: 3500},
		},
	}
	out := fp.FormatMarkdown()
	if !strings.Contains(out, "## Languages") {
		t.Error("expected Languages section")
	}
	if !strings.Contains(out, "| Language | % | Files | Lines |") {
		t.Error("expected language table header")
	}
	if !strings.Contains(out, "| Go | 70.0% | 10 | 3500 |") {
		t.Error("expected Go language row")
	}
}

func TestFormatMarkdown_GitSection(t *testing.T) {
	fp := &Fingerprint{
		Name:       "gitted",
		TotalFiles: 10,
		TotalLines: 500,
		GitInfo: &GitInfo{
			Branch:       "main",
			CommitCount:  50,
			Contributors: 3,
			LastCommit:   "fix: bug",
		},
	}
	out := fp.FormatMarkdown()
	if !strings.Contains(out, "## Git") {
		t.Error("expected Git section")
	}
	if !strings.Contains(out, "**Branch:** main") {
		t.Error("expected branch info")
	}
	if !strings.Contains(out, "**Commits:** 50") {
		t.Error("expected commit count")
	}
}

func TestFormatMarkdown_LimitsLanguageTableTo10(t *testing.T) {
	langs := make([]LanguageInfo, 15)
	for i := range langs {
		langs[i] = LanguageInfo{
			Name:       "Lang",
			Percentage: 1.0,
			Files:      1,
			Lines:      100 - i,
		}
	}
	fp := &Fingerprint{
		Name:       "multilang",
		TotalFiles: 15,
		TotalLines: 1500,
		Languages:  langs,
	}
	out := fp.FormatMarkdown()
	// Should have at most 10 language rows (check for the 11th not being present)
	// Each row contains "Lang |" so count them
	count := strings.Count(out, "| Lang |")
	if count > 10 {
		t.Errorf("expected at most 10 language rows, got %d", count)
	}
}

func TestFormatMarkdown_WithoutGitInfo(t *testing.T) {
	fp := &Fingerprint{
		Name:       "nogit",
		TotalFiles: 5,
		TotalLines: 100,
	}
	out := fp.FormatMarkdown()
	if strings.Contains(out, "## Git") {
		t.Error("should not have Git section when GitInfo is nil")
	}
}

func TestFormatMarkdown_WithoutLanguages(t *testing.T) {
	fp := &Fingerprint{
		Name:       "nolang",
		TotalFiles: 0,
		TotalLines: 0,
	}
	out := fp.FormatMarkdown()
	if strings.Contains(out, "## Languages") {
		t.Error("should not have Languages section when empty")
	}
}
