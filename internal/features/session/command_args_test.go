package session

import "testing"

func TestParseExportFormat(t *testing.T) {
	for _, test := range []struct{ input, want string }{
		{"markdown", "md"}, {"JSON", "json"}, {"text", "txt"},
	} {
		if got, ok := ParseExportFormat(test.input); !ok || got != test.want {
			t.Errorf("ParseExportFormat(%q) = %q, %v", test.input, got, ok)
		}
	}
	if _, ok := ParseExportFormat("yaml"); ok {
		t.Fatal("yaml should be rejected")
	}
}

func TestParseSessionCounts(t *testing.T) {
	if got := ParsePositiveDays([]string{"0"}, 30); got != 30 {
		t.Fatalf("invalid days = %d, want 30", got)
	}
	if got, err := ParsePositiveCount([]string{"3"}, 1); err != nil || got != 3 {
		t.Fatalf("count = %d, %v", got, err)
	}
	if _, err := ParsePositiveCount([]string{"-1"}, 1); err == nil {
		t.Fatal("negative count should fail")
	}
}

func TestSearchQuery(t *testing.T) {
	if got := SearchQuery("/search   architecture "); got != "architecture" {
		t.Fatalf("SearchQuery() = %q", got)
	}
}
