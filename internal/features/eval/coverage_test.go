package eval

import (
	"strings"
	"testing"

	"github.com/GrayCodeAI/rho/internal/ui/icons"
)

func TestParseCoverageProfile(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		wantErr        bool
		wantFiles      int
		wantPercentage float64
	}{
		{
			name:    "empty profile",
			input:   "",
			wantErr: true,
		},
		{
			name:           "mode line only",
			input:          "mode: set",
			wantErr:        false,
			wantFiles:      0,
			wantPercentage: 0,
		},
		{
			name: "single file fully covered",
			input: `mode: set
github.com/example/pkg/foo.go:10.2,12.3 1 1
github.com/example/pkg/foo.go:14.2,16.3 1 1`,
			wantErr:        false,
			wantFiles:      1,
			wantPercentage: 100,
		},
		{
			name: "single file partially covered",
			input: `mode: set
github.com/example/pkg/bar.go:5.2,8.3 1 1
github.com/example/pkg/bar.go:10.2,13.3 1 0`,
			wantErr:        false,
			wantFiles:      1,
			wantPercentage: 50,
		},
		{
			name: "multiple files",
			input: `mode: count
github.com/example/pkg/a.go:1.2,4.3 1 1
github.com/example/pkg/b.go:1.2,4.3 1 0`,
			wantErr:   false,
			wantFiles: 2,
		},
		{
			name: "overlapping ranges covered wins",
			input: `mode: set
github.com/example/pkg/c.go:5.1,10.1 1 0
github.com/example/pkg/c.go:5.1,10.1 1 1`,
			wantErr:        false,
			wantFiles:      1,
			wantPercentage: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report, err := ParseCoverageProfile(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(report.Files) != tt.wantFiles {
				t.Errorf("files count = %d, want %d", len(report.Files), tt.wantFiles)
			}

			if tt.wantPercentage > 0 && report.Percentage != tt.wantPercentage {
				t.Errorf("percentage = %.1f, want %.1f", report.Percentage, tt.wantPercentage)
			}
		})
	}
}

func TestParseCoverageProfileUncoveredRanges(t *testing.T) {
	input := `mode: set
github.com/example/pkg/foo.go:5.1,7.1 1 0
github.com/example/pkg/foo.go:8.1,8.1 1 1
github.com/example/pkg/foo.go:9.1,12.1 1 0`

	report, err := ParseCoverageProfile(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(report.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(report.Files))
	}

	fc := report.Files[0]
	if len(fc.UncoveredRanges) != 2 {
		t.Fatalf("expected 2 uncovered ranges, got %d", len(fc.UncoveredRanges))
	}

	// First range: lines 5-7.
	if fc.UncoveredRanges[0].Start != 5 || fc.UncoveredRanges[0].End != 7 {
		t.Errorf("range[0] = %d-%d, want 5-7",
			fc.UncoveredRanges[0].Start, fc.UncoveredRanges[0].End)
	}

	// Second range: lines 9-12.
	if fc.UncoveredRanges[1].Start != 9 || fc.UncoveredRanges[1].End != 12 {
		t.Errorf("range[1] = %d-%d, want 9-12",
			fc.UncoveredRanges[1].Start, fc.UncoveredRanges[1].End)
	}
}

func TestParseLineFromPos(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"10.5", 10},
		{"1.1", 1},
		{"100.23", 100},
		{"42", 42},
		{"", 0},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseLineFromPos(tt.input)
			if got != tt.want {
				t.Errorf("parseLineFromPos(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestBuildRanges(t *testing.T) {
	tests := []struct {
		name  string
		lines []int
		want  []LineRange
	}{
		{
			name:  "empty",
			lines: nil,
			want:  nil,
		},
		{
			name:  "single line",
			lines: []int{5},
			want:  []LineRange{{Start: 5, End: 5}},
		},
		{
			name:  "consecutive",
			lines: []int{3, 4, 5, 6},
			want:  []LineRange{{Start: 3, End: 6}},
		},
		{
			name:  "multiple ranges",
			lines: []int{1, 2, 3, 7, 8, 15},
			want: []LineRange{
				{Start: 1, End: 3},
				{Start: 7, End: 8},
				{Start: 15, End: 15},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildRanges(tt.lines)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d ranges, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i].Start != tt.want[i].Start || got[i].End != tt.want[i].End {
					t.Errorf("range[%d] = %d-%d, want %d-%d",
						i, got[i].Start, got[i].End, tt.want[i].Start, tt.want[i].End)
				}
			}
		})
	}
}

func TestClassifyFunction(t *testing.T) {
	tests := []struct {
		funcName     string
		wantPriority string
		wantReason   string
	}{
		{"HandleRequest", "HIGH", "exported, HTTP handler"},
		{"ParseConfig", "HIGH", "exported, has error return"},
		{"ValidateInput", "HIGH", "exported, has error return"},
		{"NewService", "HIGH", "exported, constructor"},
		{"FormatOutput", "MED", "exported"},
		{"helperFunc", "LOW", "unexported helper"},
		{"Receiver.HandleWebhook", "HIGH", "exported, HTTP handler"},
		{"Receiver.doInternal", "LOW", "unexported helper"},
	}

	for _, tt := range tests {
		t.Run(tt.funcName, func(t *testing.T) {
			priority, reason := classifyFunction(tt.funcName)
			if priority != tt.wantPriority {
				t.Errorf("priority = %q, want %q", priority, tt.wantPriority)
			}
			if reason != tt.wantReason {
				t.Errorf("reason = %q, want %q", reason, tt.wantReason)
			}
		})
	}
}

func TestSuggestTests(t *testing.T) {
	uncovered := []string{
		"ParseConfig (config/validate.go:42)",
		"helperFunc (util/parse.go:10)",
		"HandleWebhook (handler/api.go:89)",
	}

	suggestions := SuggestTests(uncovered)

	if len(suggestions) != 3 {
		t.Fatalf("expected 3 suggestions, got %d", len(suggestions))
	}

	// Verify sorted by priority (HIGH first).
	if suggestions[0].Priority != "HIGH" {
		t.Errorf("first suggestion priority = %q, want HIGH", suggestions[0].Priority)
	}
	if suggestions[len(suggestions)-1].Priority != "LOW" {
		t.Errorf("last suggestion priority = %q, want LOW", suggestions[len(suggestions)-1].Priority)
	}

	// Verify templates are non-empty.
	for i, s := range suggestions {
		if s.Template == "" {
			t.Errorf("suggestion[%d].Template is empty", i)
		}
		if !strings.Contains(s.Template, "func Test") {
			t.Errorf("suggestion[%d].Template missing test function", i)
		}
	}
}

func TestGenerateTestTemplate(t *testing.T) {
	template := GenerateTestTemplate("ParseConfig", "config", "func ParseConfig(path string) error")

	if !strings.Contains(template, "func TestParseConfig(t *testing.T)") {
		t.Error("template missing function signature")
	}
	if !strings.Contains(template, "tests := []struct") {
		t.Error("template missing test table")
	}
	if !strings.Contains(template, "t.Run(tt.name") {
		t.Error("template missing subtests")
	}
	if !strings.Contains(template, "// TODO: implement") {
		t.Error("template missing TODO placeholder")
	}
}

func TestFormatReport(t *testing.T) {
	report := &CoverageReport{
		TotalLines:   1000,
		CoveredLines: 723,
		Percentage:   72.3,
		Files: []FileCoverage{
			{Path: "src/auth/token.go", TotalLines: 100, CoveredLines: 92, Percentage: 92},
			{Path: "src/handler/api.go", TotalLines: 100, CoveredLines: 78, Percentage: 78},
			{Path: "src/config/validate.go", TotalLines: 100, CoveredLines: 45, Percentage: 45},
			{Path: "src/util/parse.go", TotalLines: 100, CoveredLines: 0, Percentage: 0},
		},
		UncoveredFunctions: []string{
			"ParseConfig (config/validate.go:42)",
			"HandleWebhook (handler/api.go:89)",
		},
		Suggestions: []TestSuggestion{
			{Function: "ParseConfig", Priority: "HIGH", Reason: "exported, has error return"},
			{Function: "HandleWebhook", Priority: "HIGH", Reason: "exported, HTTP handler"},
		},
	}

	output := FormatReport(report)

	// Check header.
	if !strings.Contains(output, "Test Coverage Report:") {
		t.Error("missing report header")
	}
	if !strings.Contains(output, "72.3%") {
		t.Error("missing overall percentage")
	}

	// Check file listing.
	if !strings.Contains(output, "src/auth/token.go") {
		t.Error("missing file entry")
	}

	// Check visual indicators.
	if !strings.Contains(output, "█") {
		t.Error("missing filled bar characters")
	}
	if !strings.Contains(output, "░") {
		t.Error("missing empty bar characters")
	}

	// Check warning indicators.
	if !strings.Contains(output, icons.CloseThick()) {
		t.Error("missing zero-coverage indicator")
	}
	if !strings.Contains(output, icons.Alert()) {
		t.Error("missing low-coverage warning")
	}

	// Check uncovered functions section.
	if !strings.Contains(output, "Uncovered functions (2):") {
		t.Error("missing uncovered functions header")
	}
	if !strings.Contains(output, "ParseConfig") {
		t.Error("missing uncovered function name")
	}

	// Check suggestions section.
	if !strings.Contains(output, "Suggestions:") {
		t.Error("missing suggestions header")
	}
	if !strings.Contains(output, "[HIGH]") {
		t.Error("missing priority label")
	}
}

func TestFormatReportNil(t *testing.T) {
	output := FormatReport(nil)
	if !strings.Contains(output, "No coverage data") {
		t.Errorf("expected nil report message, got: %s", output)
	}
}

func TestDeltaCoverage(t *testing.T) {
	before := &CoverageReport{
		TotalLines:   1000,
		CoveredLines: 700,
		Percentage:   70.0,
		Files: []FileCoverage{
			{Path: "pkg/a.go", Percentage: 80},
			{Path: "pkg/b.go", Percentage: 60},
		},
	}

	after := &CoverageReport{
		TotalLines:   1000,
		CoveredLines: 750,
		Percentage:   75.0,
		Files: []FileCoverage{
			{Path: "pkg/a.go", Percentage: 85},
			{Path: "pkg/b.go", Percentage: 60},
			{Path: "pkg/c.go", Percentage: 100},
		},
	}

	output := DeltaCoverage(before, after)

	if !strings.Contains(output, "Coverage Delta:") {
		t.Error("missing delta header")
	}
	if !strings.Contains(output, "70.0%") {
		t.Error("missing before percentage")
	}
	if !strings.Contains(output, "75.0%") {
		t.Error("missing after percentage")
	}
	if !strings.Contains(output, "▲") {
		t.Error("missing increase indicator")
	}
	if !strings.Contains(output, "pkg/c.go") {
		t.Error("missing new file entry")
	}
	if !strings.Contains(output, "(new)") {
		t.Error("missing new file indicator")
	}
}

func TestDeltaCoverageNilInputs(t *testing.T) {
	output := DeltaCoverage(nil, &CoverageReport{})
	if !strings.Contains(output, "Cannot compute delta") {
		t.Error("expected nil handling message")
	}

	output = DeltaCoverage(&CoverageReport{}, nil)
	if !strings.Contains(output, "Cannot compute delta") {
		t.Error("expected nil handling message")
	}
}

func TestDeltaCoverageDecrease(t *testing.T) {
	before := &CoverageReport{
		TotalLines:   100,
		CoveredLines: 80,
		Percentage:   80.0,
		Files: []FileCoverage{
			{Path: "pkg/a.go", Percentage: 80},
		},
	}

	after := &CoverageReport{
		TotalLines:   150,
		CoveredLines: 90,
		Percentage:   60.0,
		Files: []FileCoverage{
			{Path: "pkg/a.go", Percentage: 60},
		},
	}

	output := DeltaCoverage(before, after)
	if !strings.Contains(output, "▼") {
		t.Error("missing decrease indicator")
	}
}

func TestRenderBar(t *testing.T) {
	tests := []struct {
		percentage float64
		width      int
		wantFull   int
		wantEmpty  int
	}{
		{100, 10, 10, 0},
		{0, 10, 0, 10},
		{50, 10, 5, 5},
		{75, 20, 15, 5},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			bar := renderBar(tt.percentage, tt.width)
			full := strings.Count(bar, "█")
			empty := strings.Count(bar, "░")
			if full != tt.wantFull {
				t.Errorf("filled blocks = %d, want %d", full, tt.wantFull)
			}
			if empty != tt.wantEmpty {
				t.Errorf("empty blocks = %d, want %d", empty, tt.wantEmpty)
			}
		})
	}
}

func TestFormatNumber(t *testing.T) {
	tests := []struct {
		input int
		want  string
	}{
		{0, "0"},
		{42, "42"},
		{999, "999"},
		{1000, "1,000"},
		{1234, "1,234"},
		{1234567, "1,234,567"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatNumber(tt.input)
			if got != tt.want {
				t.Errorf("formatNumber(%d) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNewCoverageAnalyzer(t *testing.T) {
	ca := NewCoverageAnalyzer("/tmp/project")
	if ca == nil {
		t.Fatal("NewCoverageAnalyzer returned nil")
	}
	if ca.ProjectDir != "/tmp/project" {
		t.Errorf("ProjectDir = %q, want %q", ca.ProjectDir, "/tmp/project")
	}
}

func TestFindUncoveredFunctionsNilProfile(t *testing.T) {
	result := FindUncoveredFunctions(nil, "/tmp")
	if result != nil {
		t.Errorf("expected nil for nil profile, got %v", result)
	}
}

func TestExtractBaseName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"FuncName", "FuncName"},
		{"Receiver.Method", "Method"},
		{"*Receiver.Method", "Method"},
		{"pkg.Func", "Func"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := extractBaseName(tt.input)
			if got != tt.want {
				t.Errorf("extractBaseName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
