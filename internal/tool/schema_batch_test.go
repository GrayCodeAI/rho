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

func TestBatchExecSchemaProvider(t *testing.T) {
	var _ SchemaProvider = BatchExecTool{}
	props := schemaProps(t, BatchExecTool{}.Parameters())
	action, ok := props["action"].(map[string]interface{})
	if !ok || action["type"] != "string" {
		t.Fatalf("action prop = %v, want string type", props["action"])
	}
	enum, ok := action["enum"].([]interface{})
	if !ok || len(enum) != 3 || enum[0] != "submit" || enum[2] != "wait" {
		t.Fatalf("action enum = %v, want [submit poll wait]", action["enum"])
	}
	prompts, ok := props["prompts"].(map[string]interface{})
	if !ok || prompts["type"] != "array" {
		t.Fatalf("prompts prop = %v, want array type", props["prompts"])
	}
	if prompts["items"].(map[string]interface{})["type"] != "string" {
		t.Fatalf("prompts items = %v, want string", prompts["items"])
	}
	req, _ := BatchExecTool{}.Parameters()["required"].([]string)
	if len(req) != 1 || req[0] != "action" {
		t.Fatalf("required = %v, want [action]", BatchExecTool{}.Parameters()["required"])
	}
}

func TestDiagnosticsSchemaProvider(t *testing.T) {
	var _ SchemaProvider = DiagnosticsTool{}
	props := schemaProps(t, DiagnosticsTool{}.Parameters())
	if props["path"].(map[string]interface{})["type"] != "string" {
		t.Fatal("path type wrong")
	}
	enum, ok := props["scope"].(map[string]interface{})["enum"].([]interface{})
	if !ok || len(enum) != 2 || enum[0] != "file" || enum[1] != "project" {
		t.Fatalf("scope enum = %v, want [file project]", props["scope"])
	}
	req, _ := DiagnosticsTool{}.Parameters()["required"].([]string)
	if len(req) != 1 || req[0] != "path" {
		t.Fatalf("required = %v, want [path]", DiagnosticsTool{}.Parameters()["required"])
	}
}

func TestCodeSearchSchemaProvider(t *testing.T) {
	var _ SchemaProvider = CodeSearchTool{}
	props := schemaProps(t, CodeSearchTool{}.Parameters())
	for field, want := range map[string]string{"query": "string", "limit": "integer", "language": "string", "refresh": "boolean"} {
		if props[field].(map[string]interface{})["type"] != want {
			t.Fatalf("%s type = %v, want %s", field, props[field], want)
		}
	}
	req, _ := CodeSearchTool{}.Parameters()["required"].([]string)
	if len(req) != 1 || req[0] != "query" {
		t.Fatalf("required = %v, want [query]", CodeSearchTool{}.Parameters()["required"])
	}
}

func TestCodeMatchSchemaProvider(t *testing.T) {
	var _ SchemaProvider = CodeMatchTool{}
	props := schemaProps(t, CodeMatchTool{}.Parameters())
	if props["pattern"].(map[string]interface{})["type"] != "string" {
		t.Fatal("pattern type wrong")
	}
	enum, ok := props["language"].(map[string]interface{})["enum"].([]interface{})
	if !ok || len(enum) != 4 {
		t.Fatalf("language enum = %v, want 4 options", props["language"])
	}
	limit := props["limit"].(map[string]interface{})
	if limit["minimum"] != 1 || limit["maximum"] != 200 {
		t.Fatalf("limit bounds = %v/%v, want 1/200", limit["minimum"], limit["maximum"])
	}
	req, _ := CodeMatchTool{}.Parameters()["required"].([]string)
	if len(req) != 1 || req[0] != "pattern" {
		t.Fatalf("required = %v, want [pattern]", CodeMatchTool{}.Parameters()["required"])
	}
}

func TestOutlineSchemaProvider(t *testing.T) {
	var _ SchemaProvider = OutlineTool{}
	props := schemaProps(t, OutlineTool{}.Parameters())
	if props["file_path"].(map[string]interface{})["type"] != "string" {
		t.Fatal("file_path type wrong")
	}
	fp, ok := props["file_paths"].(map[string]interface{})
	if !ok || fp["type"] != "array" {
		t.Fatalf("file_paths prop = %v, want array", props["file_paths"])
	}
	if fp["items"].(map[string]interface{})["type"] != "string" {
		t.Fatalf("file_paths items = %v, want string", fp["items"])
	}
	_, hasRequired := OutlineTool{}.Parameters()["required"]
	if hasRequired {
		t.Fatalf("Outline has no required fields, got %v", OutlineTool{}.Parameters()["required"])
	}
}

func TestCronListSchemaProvider(t *testing.T) {
	var _ SchemaProvider = CronListTool{}
	props := schemaProps(t, CronListTool{}.Parameters())
	if len(props) != 0 {
		t.Fatalf("CronList has no inputs, got %v", props)
	}
	_, hasRequired := CronListTool{}.Parameters()["required"]
	if hasRequired {
		t.Fatalf("CronList has no required fields, got %v", CronListTool{}.Parameters()["required"])
	}
}

func TestBriefSchemaProvider(t *testing.T) {
	var _ SchemaProvider = BriefTool{}
	props := schemaProps(t, BriefTool{}.Parameters())
	for field, want := range map[string]string{"message": "string", "status": "string"} {
		if props[field].(map[string]interface{})["type"] != want {
			t.Fatalf("%s type = %v, want %s", field, props[field], want)
		}
	}
	att, ok := props["attachments"].(map[string]interface{})
	if !ok || att["type"] != "array" {
		t.Fatalf("attachments prop = %v, want array", props["attachments"])
	}
	if att["items"].(map[string]interface{})["type"] != "string" {
		t.Fatalf("attachments items = %v, want string", att["items"])
	}
	enum, ok := props["status"].(map[string]interface{})["enum"].([]interface{})
	if !ok || len(enum) != 2 || enum[0] != "normal" || enum[1] != "proactive" {
		t.Fatalf("status enum = %v, want [normal proactive]", props["status"])
	}
	req, _ := BriefTool{}.Parameters()["required"].([]string)
	if len(req) != 1 || req[0] != "message" {
		t.Fatalf("required = %v, want [message]", BriefTool{}.Parameters()["required"])
	}
}

func TestAPICompatSchemaProvider(t *testing.T) {
	compat := &APICompatTool{}
	var _ SchemaProvider = compat
	props := schemaProps(t, compat.Parameters())
	for field, want := range map[string]string{"package_path": "string", "baseline_path": "string", "save_baseline": "boolean"} {
		if props[field].(map[string]interface{})["type"] != want {
			t.Fatalf("%s type = %v, want %s", field, props[field], want)
		}
	}
	req, _ := compat.Parameters()["required"].([]string)
	if len(req) != 1 || req[0] != "package_path" {
		t.Fatalf("required = %v, want [package_path]", compat.Parameters()["required"])
	}
}

func TestAutoImportSchemaProvider(t *testing.T) {
	var _ SchemaProvider = AutoImportTool{}
	props := schemaProps(t, AutoImportTool{}.Parameters())
	for field, want := range map[string]string{"code": "string", "file": "string", "apply": "boolean"} {
		if props[field].(map[string]interface{})["type"] != want {
			t.Fatalf("%s type = %v, want %s", field, props[field], want)
		}
	}
	req, _ := AutoImportTool{}.Parameters()["required"].([]string)
	if len(req) != 1 || req[0] != "code" {
		t.Fatalf("required = %v, want [code]", AutoImportTool{}.Parameters()["required"])
	}
}

func TestConflictResolverSchemaProvider(t *testing.T) {
	var _ SchemaProvider = ConflictResolverTool{}
	props := schemaProps(t, ConflictResolverTool{}.Parameters())
	if props["path"].(map[string]interface{})["type"] != "string" {
		t.Fatal("path type wrong")
	}
	enum, ok := props["strategy"].(map[string]interface{})["enum"].([]interface{})
	if !ok || len(enum) != 3 || enum[0] != "smart" || enum[2] != "theirs" {
		t.Fatalf("strategy enum = %v, want [smart ours theirs]", props["strategy"])
	}
	if props["dry_run"].(map[string]interface{})["type"] != "boolean" {
		t.Fatal("dry_run type wrong")
	}
	req, _ := ConflictResolverTool{}.Parameters()["required"].([]string)
	if len(req) != 1 || req[0] != "path" {
		t.Fatalf("required = %v, want [path]", ConflictResolverTool{}.Parameters()["required"])
	}
}

func TestAppVerifySchemaProvider(t *testing.T) {
	var _ SchemaProvider = AppVerifyTool{}
	props := schemaProps(t, AppVerifyTool{}.Parameters())
	action, ok := props["action"].(map[string]interface{})
	if !ok || action["type"] != "string" {
		t.Fatalf("action prop = %v, want string", props["action"])
	}
	enum, ok := action["enum"].([]interface{})
	if !ok || len(enum) != 3 || enum[0] != "detect" || enum[2] != "smoke" {
		t.Fatalf("action enum = %v, want [detect manifest smoke]", action["enum"])
	}
	rs := props["readiness_seconds"].(map[string]interface{})
	if rs["minimum"] != 1 || rs["maximum"] != 300 {
		t.Fatalf("readiness_seconds bounds = %v/%v, want 1/300", rs["minimum"], rs["maximum"])
	}
	req, _ := AppVerifyTool{}.Parameters()["required"].([]string)
	if len(req) != 1 || req[0] != "action" {
		t.Fatalf("required = %v, want [action]", AppVerifyTool{}.Parameters()["required"])
	}
}

func TestRequestCredentialSchemaProvider(t *testing.T) {
	compat := &RequestCredentialTool{}
	var _ SchemaProvider = compat
	props := schemaProps(t, compat.Parameters())
	if props["credential"].(map[string]interface{})["type"] != "string" {
		t.Fatal("credential type wrong")
	}
	if props["reason"].(map[string]interface{})["type"] != "string" {
		t.Fatal("reason type wrong")
	}
	req, _ := compat.Parameters()["required"].([]string)
	if len(req) != 2 || req[0] != "credential" || req[1] != "reason" {
		t.Fatalf("required = %v, want [credential reason]", compat.Parameters()["required"])
	}
}

func TestDebuggerSchemaProvider(t *testing.T) {
	var _ SchemaProvider = DebuggerTool{}
	props := schemaProps(t, DebuggerTool{}.Parameters())
	action, ok := props["action"].(map[string]interface{})
	if !ok || action["type"] != "string" {
		t.Fatalf("action prop = %v, want string", props["action"])
	}
	enum, ok := action["enum"].([]interface{})
	if !ok || len(enum) != 6 || enum[0] != "breakpoint" || enum[5] != "stack" {
		t.Fatalf("action enum = %v, want 6 options", action["enum"])
	}
	req, _ := DebuggerTool{}.Parameters()["required"].([]string)
	if len(req) != 1 || req[0] != "action" {
		t.Fatalf("required = %v, want [action]", DebuggerTool{}.Parameters()["required"])
	}
}

func TestDependencyAuditSchemaProvider(t *testing.T) {
	var _ SchemaProvider = DependencyAuditTool{}
	props := schemaProps(t, DependencyAuditTool{}.Parameters())
	enum, ok := props["action"].(map[string]interface{})["enum"].([]interface{})
	if !ok || len(enum) != 3 || enum[0] != "check" {
		t.Fatalf("action enum = %v, want [check outdated all]", props["action"])
	}
	ts := props["timeout_seconds"].(map[string]interface{})
	if ts["minimum"] != 1 || ts["maximum"] != 300 {
		t.Fatalf("timeout_seconds bounds = %v/%v, want 1/300", ts["minimum"], ts["maximum"])
	}
	req, _ := DependencyAuditTool{}.Parameters()["required"].([]string)
	if len(req) != 1 || req[0] != "action" {
		t.Fatalf("required = %v, want [action]", DependencyAuditTool{}.Parameters()["required"])
	}
}

func TestGitSchemaProvider(t *testing.T) {
	var _ SchemaProvider = GitTool{}
	props := schemaProps(t, GitTool{}.Parameters())
	if props["subcommand"].(map[string]interface{})["type"] != "string" {
		t.Fatal("subcommand type wrong")
	}
	args, ok := props["args"].(map[string]interface{})
	if !ok || args["type"] != "array" {
		t.Fatalf("args prop = %v, want array", props["args"])
	}
	if args["items"].(map[string]interface{})["type"] != "string" {
		t.Fatalf("args items = %v, want string", args["items"])
	}
	req, _ := GitTool{}.Parameters()["required"].([]string)
	if len(req) != 1 || req[0] != "subcommand" {
		t.Fatalf("required = %v, want [subcommand]", GitTool{}.Parameters()["required"])
	}
}

func TestGitHubSchemaProvider(t *testing.T) {
	var _ SchemaProvider = GitHubTool{}
	props := schemaProps(t, GitHubTool{}.Parameters())
	enum, ok := props["action"].(map[string]interface{})["enum"].([]interface{})
	if !ok || len(enum) != 9 {
		t.Fatalf("action enum = %v, want 9 options", props["action"])
	}
	l := props["limit"].(map[string]interface{})
	if l["minimum"] != 1 || l["maximum"] != 50 {
		t.Fatalf("limit bounds = %v/%v, want 1/50", l["minimum"], l["maximum"])
	}
	req, _ := GitHubTool{}.Parameters()["required"].([]string)
	if len(req) != 1 || req[0] != "action" {
		t.Fatalf("required = %v, want [action]", GitHubTool{}.Parameters()["required"])
	}
}

func TestImportOrganizerSchemaProvider(t *testing.T) {
	var _ SchemaProvider = ImportOrganizerTool{}
	props := schemaProps(t, ImportOrganizerTool{}.Parameters())
	if props["path"].(map[string]interface{})["type"] != "string" {
		t.Fatal("path type wrong")
	}
	req, _ := ImportOrganizerTool{}.Parameters()["required"].([]string)
	if len(req) != 1 || req[0] != "path" {
		t.Fatalf("required = %v, want [path]", ImportOrganizerTool{}.Parameters()["required"])
	}
}

func TestJobsSchemaProvider(t *testing.T) {
	var _ SchemaProvider = JobsTool{}
	props := schemaProps(t, JobsTool{}.Parameters())
	enum, ok := props["action"].(map[string]interface{})["enum"].([]interface{})
	if !ok || len(enum) != 5 || enum[0] != "list" || enum[4] != "kill" {
		t.Fatalf("action enum = %v, want 5 options", props["action"])
	}
	if props["command"].(map[string]interface{})["type"] != "string" {
		t.Fatal("command type wrong")
	}
	req, _ := JobsTool{}.Parameters()["required"].([]string)
	if len(req) != 1 || req[0] != "action" {
		t.Fatalf("required = %v, want [action]", JobsTool{}.Parameters()["required"])
	}
}

func TestLSPToolSchemaProvider(t *testing.T) {
	var _ SchemaProvider = LSPTool{}
	props := schemaProps(t, LSPTool{}.Parameters())
	enum, ok := props["action"].(map[string]interface{})["enum"].([]interface{})
	if !ok || len(enum) != 4 {
		t.Fatalf("action enum = %v, want 4 options", props["action"])
	}
	req, _ := LSPTool{}.Parameters()["required"].([]string)
	if len(req) != 2 || req[0] != "action" || req[1] != "path" {
		t.Fatalf("required = %v, want [action path]", LSPTool{}.Parameters()["required"])
	}
}

func TestToolSearchSchemaProvider(t *testing.T) {
	var _ SchemaProvider = ToolSearchTool{}
	props := schemaProps(t, ToolSearchTool{}.Parameters())
	if props["query"].(map[string]interface{})["type"] != "string" {
		t.Fatal("query type wrong")
	}
	if props["max_results"].(map[string]interface{})["type"] != "integer" {
		t.Fatal("max_results type wrong")
	}
	req, _ := ToolSearchTool{}.Parameters()["required"].([]string)
	if len(req) != 1 || req[0] != "query" {
		t.Fatalf("required = %v, want [query]", ToolSearchTool{}.Parameters()["required"])
	}
}

func TestToolsetSchemaProvider(t *testing.T) {
	var _ SchemaProvider = ToolsetTool{}
	props := schemaProps(t, ToolsetTool{}.Parameters())
	enum, ok := props["action"].(map[string]interface{})["enum"].([]interface{})
	if !ok || len(enum) != 2 || enum[0] != "list" || enum[1] != "resolve" {
		t.Fatalf("action enum = %v, want [list resolve]", props["action"])
	}
	req, _ := ToolsetTool{}.Parameters()["required"].([]string)
	if len(req) != 1 || req[0] != "action" {
		t.Fatalf("required = %v, want [action]", ToolsetTool{}.Parameters()["required"])
	}
}

func TestWorktreeSchemasProvider(t *testing.T) {
	var _ SchemaProvider = EnterWorktreeTool{}
	var _ SchemaProvider = ExitWorktreeTool{}
	props := schemaProps(t, EnterWorktreeTool{}.Parameters())
	if props["path"].(map[string]interface{})["type"] != "string" {
		t.Fatal("enter path type wrong")
	}
	req, _ := EnterWorktreeTool{}.Parameters()["required"].([]string)
	if len(req) != 1 || req[0] != "path" {
		t.Fatalf("enter required = %v, want [path]", EnterWorktreeTool{}.Parameters()["required"])
	}
	eprops := schemaProps(t, ExitWorktreeTool{}.Parameters())
	if eprops["cleanup"].(map[string]interface{})["type"] != "boolean" {
		t.Fatal("exit cleanup type wrong")
	}
}

func TestPowerShellSchemaProvider(t *testing.T) {
	var _ SchemaProvider = PowerShellTool{}
	props := schemaProps(t, PowerShellTool{}.Parameters())
	if props["command"].(map[string]interface{})["type"] != "string" {
		t.Fatal("command type wrong")
	}
	if props["timeout"].(map[string]interface{})["type"] != "number" {
		t.Fatal("timeout type wrong")
	}
	req, _ := PowerShellTool{}.Parameters()["required"].([]string)
	if len(req) != 1 || req[0] != "command" {
		t.Fatalf("required = %v, want [command]", PowerShellTool{}.Parameters()["required"])
	}
}
