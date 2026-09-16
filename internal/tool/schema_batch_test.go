package tool

import "testing"

func schemaProps(t *testing.T, params map[string]interface{}) map[string]interface{} {
	t.Helper()
	props, ok := params["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("properties missing")
	}
	return props
}

func TestGlobSchemaProvider(t *testing.T) {
	var _ SchemaProvider = GlobTool{}
	props := schemaProps(t, GlobTool{}.Parameters())
	if props["pattern"].(map[string]interface{})["type"] != "string" {
		t.Fatal("pattern type wrong")
	}
	if props["path"].(map[string]interface{})["type"] != "string" {
		t.Fatal("path type wrong")
	}
	req, _ := GlobTool{}.Parameters()["required"].([]string)
	if len(req) != 1 || req[0] != "pattern" {
		t.Fatalf("required = %v, want [pattern]", GlobTool{}.Parameters()["required"])
	}
}

func TestLSSchemaProvider(t *testing.T) {
	var _ SchemaProvider = LSTool{}
	props := schemaProps(t, LSTool{}.Parameters())
	ignore, ok := props["ignore"].(map[string]interface{})
	if !ok || ignore["type"] != "array" {
		t.Fatalf("ignore prop = %v, want array type", props["ignore"])
	}
	items, ok := ignore["items"].(map[string]interface{})
	if !ok || items["type"] != "string" {
		t.Fatalf("ignore items = %v, want string type", ignore["items"])
	}
	_, hasRequired := LSTool{}.Parameters()["required"]
	if hasRequired {
		t.Fatalf("LS has no required fields, got %v", LSTool{}.Parameters()["required"])
	}
}

func TestGrepSchemaProvider(t *testing.T) {
	var _ SchemaProvider = GrepTool{}
	props := schemaProps(t, GrepTool{}.Parameters())
	for _, f := range []string{"pattern", "path", "include"} {
		if props[f].(map[string]interface{})["type"] != "string" {
			t.Fatalf("%s type wrong", f)
		}
	}
	req, _ := GrepTool{}.Parameters()["required"].([]string)
	if len(req) != 1 || req[0] != "pattern" {
		t.Fatalf("required = %v, want [pattern]", GrepTool{}.Parameters()["required"])
	}
}

func TestSkillSchemaProvider(t *testing.T) {
	var _ SchemaProvider = SkillTool{}
	props := schemaProps(t, SkillTool{}.Parameters())
	if props["skill"].(map[string]interface{})["type"] != "string" {
		t.Fatal("skill type wrong")
	}
	_, hasRequired := SkillTool{}.Parameters()["required"]
	if hasRequired {
		t.Fatalf("Skill has no required fields, got %v", SkillTool{}.Parameters()["required"])
	}
}

func TestScreenshotSchemaProvider(t *testing.T) {
	var _ SchemaProvider = ScreenshotTool{}
	props := schemaProps(t, ScreenshotTool{}.Parameters())
	if props["url"].(map[string]interface{})["type"] != "string" {
		t.Fatal("url type wrong")
	}
	if props["width"].(map[string]interface{})["type"] != "number" {
		t.Fatal("width type wrong")
	}
	req, _ := ScreenshotTool{}.Parameters()["required"].([]string)
	if len(req) != 1 || req[0] != "url" {
		t.Fatalf("required = %v, want [url]", ScreenshotTool{}.Parameters()["required"])
	}
}

func TestFuzzyFindSchemaProvider(t *testing.T) {
	var _ SchemaProvider = FuzzyFindTool{}
	props := schemaProps(t, FuzzyFindTool{}.Parameters())
	limit, ok := props["limit"].(map[string]interface{})
	if !ok || limit["type"] != "integer" {
		t.Fatalf("limit prop = %v, want integer type", props["limit"])
	}
	// Bounds must survive the migration verbatim.
	if limit["minimum"] != 1 || limit["maximum"] != 100 {
		t.Fatalf("limit bounds = %v/%v, want 1/100", limit["minimum"], limit["maximum"])
	}
	req, _ := FuzzyFindTool{}.Parameters()["required"].([]string)
	if len(req) != 1 || req[0] != "query" {
		t.Fatalf("required = %v, want [query]", FuzzyFindTool{}.Parameters()["required"])
	}
}

func TestMatchesRange(t *testing.T) {
	if !matchesRange("x", 1, 100) {
		t.Fatal("non-numeric value should pass (only numbers are ranged)")
	}
	if !matchesRange(float64(50), 1, 100) {
		t.Fatal("50 should be in range")
	}
	if matchesRange(float64(0), 1, 100) {
		t.Fatal("0 should be out of range")
	}
	if matchesRange(float64(101), 1, 100) {
		t.Fatal("101 should be out of range")
	}
	if !matchesRange(float64(5), nil, nil) {
		t.Fatal("no bounds should pass")
	}
}

func TestDownloadSchemaProvider(t *testing.T) {
	var _ SchemaProvider = DownloadTool{}
	props := schemaProps(t, DownloadTool{}.Parameters())
	for _, f := range []string{"url", "destination"} {
		if props[f].(map[string]interface{})["type"] != "string" {
			t.Fatalf("%s type wrong", f)
		}
	}
	_, hasRequired := DownloadTool{}.Parameters()["required"]
	if hasRequired {
		t.Fatalf("Download has no required fields, got %v", DownloadTool{}.Parameters()["required"])
	}
}

func TestWebFetchSchemaProvider(t *testing.T) {
	var _ SchemaProvider = WebFetchTool{}
	props := schemaProps(t, WebFetchTool{}.Parameters())
	if props["url"].(map[string]interface{})["type"] != "string" {
		t.Fatal("url type wrong")
	}
	req, _ := WebFetchTool{}.Parameters()["required"].([]string)
	if len(req) != 1 || req[0] != "url" {
		t.Fatalf("required = %v, want [url]", WebFetchTool{}.Parameters()["required"])
	}
}

func TestFileWriteSchemaProvider(t *testing.T) {
	var _ SchemaProvider = FileWriteTool{}
	props := schemaProps(t, FileWriteTool{}.Parameters())
	// The file_path alias marker must survive: the validator discovers
	// aliases from descriptions.
	if desc := props["file_path"].(map[string]interface{})["description"]; desc != "Archive-compatible alias for path" {
		t.Fatalf("file_path description = %v, alias marker lost", desc)
	}
	req, _ := FileWriteTool{}.Parameters()["required"].([]string)
	if len(req) != 2 || req[0] != "path" || req[1] != "content" {
		t.Fatalf("required = %v, want [path content]", FileWriteTool{}.Parameters()["required"])
	}
}

func TestFileEditSchemaProvider(t *testing.T) {
	var _ SchemaProvider = FileEditTool{}
	props := schemaProps(t, FileEditTool{}.Parameters())
	for _, f := range []string{"path", "old_str", "new_str"} {
		if props[f].(map[string]interface{})["type"] != "string" {
			t.Fatalf("%s type wrong", f)
		}
	}
	if desc := props["old_string"].(map[string]interface{})["description"]; desc != "Archive-compatible alias for old_str" {
		t.Fatalf("old_string description = %v, alias marker lost", desc)
	}
	req, _ := FileEditTool{}.Parameters()["required"].([]string)
	if len(req) != 2 || req[0] != "path" || req[1] != "old_str" {
		t.Fatalf("required = %v, want [path old_str]", FileEditTool{}.Parameters()["required"])
	}
}

func TestBatchSchemaProvider(t *testing.T) {
	var _ SchemaProvider = BatchTool{}
	props := schemaProps(t, BatchTool{}.Parameters())
	calls, ok := props["calls"].(map[string]interface{})
	if !ok || calls["type"] != "array" {
		t.Fatalf("calls prop = %v, want array type", props["calls"])
	}
	items, ok := calls["items"].(map[string]interface{})
	if !ok || items["type"] != "object" {
		t.Fatalf("calls items = %v, want object type", calls["items"])
	}
	sub, ok := items["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("calls items properties missing")
	}
	if sub["tool"].(map[string]interface{})["type"] != "string" {
		t.Fatal("calls items tool type wrong")
	}
	if req, _ := items["required"].([]string); len(req) != 1 || req[0] != "tool" {
		t.Fatalf("calls items required = %v, want [tool]", items["required"])
	}
	req, _ := BatchTool{}.Parameters()["required"].([]string)
	if len(req) != 1 || req[0] != "calls" {
		t.Fatalf("required = %v, want [calls]", BatchTool{}.Parameters()["required"])
	}
}

func TestCronSchemasProvider(t *testing.T) {
	var _ SchemaProvider = CronCreateTool{}
	var _ SchemaProvider = CronDeleteTool{}
	props := schemaProps(t, CronCreateTool{}.Parameters())
	if props["schedule"].(map[string]interface{})["type"] != "string" {
		t.Fatal("schedule type wrong")
	}
	req, _ := CronCreateTool{}.Parameters()["required"].([]string)
	if len(req) != 2 || req[0] != "schedule" || req[1] != "prompt" {
		t.Fatalf("required = %v, want [schedule prompt]", CronCreateTool{}.Parameters()["required"])
	}
	dprops := schemaProps(t, CronDeleteTool{}.Parameters())
	if dprops["id"].(map[string]interface{})["type"] != "string" {
		t.Fatal("id type wrong")
	}
}

func TestNotebookEditSchemaProvider(t *testing.T) {
	var _ SchemaProvider = NotebookEditTool{}
	props := schemaProps(t, NotebookEditTool{}.Parameters())
	for _, f := range []string{"path", "new_source"} {
		if props[f].(map[string]interface{})["type"] != "string" {
			t.Fatalf("%s type wrong", f)
		}
	}
	if props["cell_number"].(map[string]interface{})["type"] != "integer" {
		t.Fatal("cell_number type wrong")
	}
}

func TestConfigSchemaProvider(t *testing.T) {
	var _ SchemaProvider = ConfigTool{}
	props := schemaProps(t, ConfigTool{}.Parameters())
	action, ok := props["action"].(map[string]interface{})
	if !ok || action["type"] != "string" {
		t.Fatalf("action prop = %v, want string type", props["action"])
	}
	enum, ok := action["enum"].([]interface{})
	if !ok || len(enum) != 2 {
		t.Fatalf("action enum = %v, want [get set]", action["enum"])
	}
}

func TestImpactSchemaProvider(t *testing.T) {
	var _ SchemaProvider = ImpactTool{}
	props := schemaProps(t, ImpactTool{}.Parameters())
	files, ok := props["files"].(map[string]interface{})
	if !ok || files["type"] != "array" {
		t.Fatalf("files prop = %v, want array type", props["files"])
	}
	items, ok := files["items"].(map[string]interface{})
	if !ok || items["type"] != "string" {
		t.Fatalf("files items = %v, want string type", files["items"])
	}
	_, hasRequired := ImpactTool{}.Parameters()["required"]
	if hasRequired {
		t.Fatalf("Impact has no required fields, got %v", ImpactTool{}.Parameters()["required"])
	}
}

func TestGitHistorySchemaProvider(t *testing.T) {
	var _ SchemaProvider = GitHistoryTool{}
	props := schemaProps(t, GitHistoryTool{}.Parameters())
	action, ok := props["action"].(map[string]interface{})
	if !ok || action["type"] != "string" {
		t.Fatalf("action prop = %v, want string type", props["action"])
	}
	enum, ok := action["enum"].([]interface{})
	if !ok || len(enum) != 4 {
		t.Fatalf("action enum = %v, want 4 options", action["enum"])
	}
	req, _ := GitHistoryTool{}.Parameters()["required"].([]string)
	if len(req) != 1 || req[0] != "action" {
		t.Fatalf("required = %v, want [action]", GitHistoryTool{}.Parameters()["required"])
	}
}
