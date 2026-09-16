package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"sort"
	"strings"
)

// ImportOrganizer organizes and manages imports for Go and TypeScript files.
type ImportOrganizer struct {
	Language   string
	GroupOrder []string
}

// ImportGroup represents a logical grouping of imports.
type ImportGroup struct {
	Name    string
	Imports []ImportEntry
	Comment string
}

// ImportEntry represents a single import statement.
type ImportEntry struct {
	Path  string
	Alias string
	Used  bool
}

// NewImportOrganizer creates a new ImportOrganizer for the given language.
func NewImportOrganizer(language string) *ImportOrganizer {
	var groupOrder []string
	switch strings.ToLower(language) {
	case "go":
		groupOrder = []string{"stdlib", "external", "internal"}
	case "typescript", "ts":
		groupOrder = []string{"builtin", "external", "internal"}
	default:
		groupOrder = []string{"stdlib", "external", "internal"}
	}
	return &ImportOrganizer{
		Language:   strings.ToLower(language),
		GroupOrder: groupOrder,
	}
}

// OrganizeGo parses and reorganizes imports in Go source code.
func (o *ImportOrganizer) OrganizeGo(content string) (string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", content, parser.ParseComments)
	if err != nil {
		return "", fmt.Errorf("parse error: %w", err)
	}

	// Detect module path from go.mod style (look for module declaration in content context).
	modulePath := detectModulePath(content)

	// Collect all imports.
	var imports []ImportEntry
	for _, imp := range f.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		alias := ""
		if imp.Name != nil {
			alias = imp.Name.Name
		}
		imports = append(imports, ImportEntry{
			Path:  path,
			Alias: alias,
			Used:  true, // Default to used; detect unused separately.
		})
	}

	if len(imports) == 0 {
		return content, nil
	}

	// Detect unused imports.
	unused := o.DetectUnusedGo(content, imports)
	unusedSet := make(map[string]bool, len(unused))
	for _, u := range unused {
		unusedSet[u.Path] = true
	}

	// Filter out unused imports.
	var filtered []ImportEntry
	for _, imp := range imports {
		if !unusedSet[imp.Path] {
			imp.Used = true
			filtered = append(filtered, imp)
		}
	}

	// Group imports.
	groups := o.groupGoImports(filtered, modulePath)

	// Format the new import block.
	newBlock := o.FormatImportBlock(groups, "go")

	// Replace old import block in the content.
	result := replaceGoImportBlock(content, newBlock)
	return result, nil
}

// OrganizeTypeScript parses and reorganizes imports in TypeScript source code.
func (o *ImportOrganizer) OrganizeTypeScript(content string) (string, error) {
	imports, importRegion := parseTypeScriptImports(content)
	if len(imports) == 0 {
		return content, nil
	}

	// Detect unused imports.
	unused := o.DetectUnusedTS(content, imports)
	unusedSet := make(map[string]bool, len(unused))
	for _, u := range unused {
		unusedSet[u.Path] = true
	}

	// Filter out unused.
	var filtered []ImportEntry
	for _, imp := range imports {
		if !unusedSet[imp.Path] {
			imp.Used = true
			filtered = append(filtered, imp)
		}
	}

	// Group imports.
	groups := o.groupTSImports(filtered)

	// Format new import block.
	newBlock := o.FormatImportBlock(groups, "typescript")

	// Replace old imports.
	result := content[:importRegion.start] + newBlock + content[importRegion.end:]
	return result, nil
}

// DetectUnusedGo checks each import against the file body to find unused ones.
func (o *ImportOrganizer) DetectUnusedGo(content string, imports []ImportEntry) []ImportEntry {
	// Find the body after the import block.
	body := getGoBody(content)

	var unused []ImportEntry
	for _, imp := range imports {
		// Blank imports are always "used".
		if imp.Alias == "_" {
			continue
		}

		pkgName := imp.Alias
		if pkgName == "" {
			// Use last element of path.
			parts := strings.Split(imp.Path, "/")
			pkgName = parts[len(parts)-1]
		}

		// Dot imports are always considered used (they import into namespace).
		if pkgName == "." {
			continue
		}

		// Check if the package name is referenced in the body.
		// Look for pkgName followed by a dot (e.g., fmt.Println).
		pattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(pkgName) + `\.`)
		if !pattern.MatchString(body) {
			unused = append(unused, imp)
		}
	}
	return unused
}

// DetectUnusedTS checks each TypeScript import for usage in the file.
func (o *ImportOrganizer) DetectUnusedTS(content string, imports []ImportEntry) []ImportEntry {
	body := getTSBody(content, imports)

	var unused []ImportEntry
	for _, imp := range imports {
		// Side-effect imports (no alias) are always used.
		if imp.Alias == "" || imp.Alias == "*" {
			continue
		}

		// Check if any imported name appears in the body.
		names := parseTSImportNames(imp.Alias)
		allUnused := true
		for _, name := range names {
			if name == "" {
				continue
			}
			pattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`)
			if pattern.MatchString(body) {
				allUnused = false
				break
			}
		}
		if allUnused && len(names) > 0 {
			unused = append(unused, imp)
		}
	}
	return unused
}

// AddMissingImport adds an import to the correct group in the content.
func (o *ImportOrganizer) AddMissingImport(content, importPath string) (string, error) {
	switch o.Language {
	case "go":
		return o.addMissingGoImport(content, importPath)
	case "typescript", "ts":
		return o.addMissingTSImport(content, importPath)
	default:
		return "", fmt.Errorf("unsupported language: %s", o.Language)
	}
}

// RemoveImport removes a specific import from the content.
func (o *ImportOrganizer) RemoveImport(content, importPath string) (string, error) {
	switch o.Language {
	case "go":
		return o.removeGoImport(content, importPath)
	case "typescript", "ts":
		return o.removeTSImport(content, importPath)
	default:
		return "", fmt.Errorf("unsupported language: %s", o.Language)
	}
}

// FormatImportBlock renders properly formatted import block for the language.
func (o *ImportOrganizer) FormatImportBlock(groups []ImportGroup, language string) string {
	switch strings.ToLower(language) {
	case "go":
		return formatGoImportBlock(groups)
	case "typescript", "ts":
		return formatTSImportBlock(groups)
	default:
		return ""
	}
}

// --- Go-specific helpers ---

func (o *ImportOrganizer) groupGoImports(imports []ImportEntry, modulePath string) []ImportGroup {
	stdlibGroup := ImportGroup{Name: "stdlib"}
	externalGroup := ImportGroup{Name: "external"}
	internalGroup := ImportGroup{Name: "internal"}

	for _, imp := range imports {
		if isGoStdlib(imp.Path) {
			stdlibGroup.Imports = append(stdlibGroup.Imports, imp)
		} else if modulePath != "" && strings.HasPrefix(imp.Path, modulePath) {
			internalGroup.Imports = append(internalGroup.Imports, imp)
		} else {
			externalGroup.Imports = append(externalGroup.Imports, imp)
		}
	}

	// Sort within groups.
	sortImports := func(imps []ImportEntry) {
		sort.Slice(imps, func(i, j int) bool {
			return imps[i].Path < imps[j].Path
		})
	}
	sortImports(stdlibGroup.Imports)
	sortImports(externalGroup.Imports)
	sortImports(internalGroup.Imports)

	var groups []ImportGroup
	if len(stdlibGroup.Imports) > 0 {
		groups = append(groups, stdlibGroup)
	}
	if len(externalGroup.Imports) > 0 {
		groups = append(groups, externalGroup)
	}
	if len(internalGroup.Imports) > 0 {
		groups = append(groups, internalGroup)
	}
	return groups
}

func isGoStdlib(path string) bool {
	// Standard library packages don't contain a dot in the first path element.
	parts := strings.SplitN(path, "/", 2)
	return !strings.Contains(parts[0], ".")
}

func formatGoImportBlock(groups []ImportGroup) string {
	if len(groups) == 0 {
		return ""
	}

	// If there's exactly one import across all groups, use single-line format.
	totalImports := 0
	for _, g := range groups {
		totalImports += len(g.Imports)
	}
	if totalImports == 1 {
		imp := groups[0].Imports[0]
		if imp.Alias != "" {
			return fmt.Sprintf("import %s %q\n", imp.Alias, imp.Path)
		}
		return fmt.Sprintf("import %q\n", imp.Path)
	}

	var b strings.Builder
	b.WriteString("import (\n")
	for i, group := range groups {
		if group.Comment != "" {
			b.WriteString("\t// " + group.Comment + "\n")
		}
		for _, imp := range group.Imports {
			if imp.Alias != "" {
				b.WriteString(fmt.Sprintf("\t%s %q\n", imp.Alias, imp.Path))
			} else {
				b.WriteString(fmt.Sprintf("\t%q\n", imp.Path))
			}
		}
		// Add blank line between groups (but not after the last one).
		if i < len(groups)-1 {
			b.WriteString("\n")
		}
	}
	b.WriteString(")\n")
	return b.String()
}

func replaceGoImportBlock(content, newBlock string) string {
	// Find the import block region using regex.
	// Match single-line import: import "path" or import alias "path"
	singleImportRe := regexp.MustCompile(`(?m)^import\s+(\w+\s+)?"[^"]+"\s*\n`)
	// Match grouped import: import ( ... )
	groupedImportRe := regexp.MustCompile(`(?ms)^import\s*\(\s*\n.*?\)\s*\n`)

	if loc := groupedImportRe.FindStringIndex(content); loc != nil {
		return content[:loc[0]] + newBlock + content[loc[1]:]
	}

	// Handle multiple single-line imports or a single import.
	locs := singleImportRe.FindAllStringIndex(content, -1)
	if len(locs) > 0 {
		start := locs[0][0]
		end := locs[len(locs)-1][1]
		return content[:start] + newBlock + content[end:]
	}

	return content
}

func getGoBody(content string) string {
	// Find the end of the import block and return everything after it.
	groupedImportRe := regexp.MustCompile(`(?ms)^import\s*\(\s*\n.*?\)\s*\n`)
	if loc := groupedImportRe.FindStringIndex(content); loc != nil {
		return content[loc[1]:]
	}
	singleImportRe := regexp.MustCompile(`(?m)^import\s+(\w+\s+)?"[^"]+"\s*\n`)
	locs := singleImportRe.FindAllStringIndex(content, -1)
	if len(locs) > 0 {
		return content[locs[len(locs)-1][1]:]
	}
	return content
}

func detectModulePath(content string) string {
	// Look for a module path hint in imports (find common prefix among non-stdlib imports).
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", content, parser.ImportsOnly)
	if err != nil {
		return ""
	}

	var nonStdlib []string
	for _, imp := range f.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		if !isGoStdlib(path) {
			nonStdlib = append(nonStdlib, path)
		}
	}

	if len(nonStdlib) == 0 {
		return ""
	}

	// Find common prefix.
	if len(nonStdlib) == 1 {
		// Use first three segments as module path guess (host/org/repo).
		parts := strings.Split(nonStdlib[0], "/")
		if len(parts) >= 3 {
			return strings.Join(parts[:3], "/")
		}
		return nonStdlib[0]
	}

	prefix := nonStdlib[0]
	for _, p := range nonStdlib[1:] {
		prefix = importCommonPrefix(prefix, p)
	}
	// Trim trailing slash.
	prefix = strings.TrimRight(prefix, "/")

	// A valid module path must have at least 3 segments (host/org/repo).
	// If the common prefix is too short (e.g., just "github.com"), it's not
	// a meaningful module path for grouping.
	parts := strings.Split(prefix, "/")
	if len(parts) < 3 {
		// Try to find the most common 3-segment prefix among the imports.
		return detectMostCommonModule(nonStdlib)
	}

	return prefix
}

// detectMostCommonModule finds the most frequently occurring 3-segment module
// prefix among a set of import paths.
func detectMostCommonModule(paths []string) string {
	counts := make(map[string]int)
	for _, p := range paths {
		parts := strings.Split(p, "/")
		if len(parts) >= 3 {
			mod := strings.Join(parts[:3], "/")
			counts[mod]++
		}
	}
	if len(counts) == 0 {
		return ""
	}

	// Return the module path that appears most frequently.
	var best string
	bestCount := 0
	for mod, count := range counts {
		if count > bestCount {
			best = mod
			bestCount = count
		}
	}
	// Only use it if it appears more than once (indicates it's likely the project module).
	if bestCount > 1 {
		return best
	}
	return ""
}

func importCommonPrefix(a, b string) string {
	aParts := strings.Split(a, "/")
	bParts := strings.Split(b, "/")
	var common []string
	for i := 0; i < len(aParts) && i < len(bParts); i++ {
		if aParts[i] == bParts[i] {
			common = append(common, aParts[i])
		} else {
			break
		}
	}
	if len(common) == 0 {
		return ""
	}
	return strings.Join(common, "/") + "/"
}

func (o *ImportOrganizer) addMissingGoImport(content, importPath string) (string, error) {
	// Check if import already exists.
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", content, parser.ImportsOnly)
	if err != nil {
		return "", fmt.Errorf("parse error: %w", err)
	}

	for _, imp := range f.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		if path == importPath {
			return content, nil // Already exists.
		}
	}

	// If there's an existing import block, add to it and reorganize.
	groupedImportRe := regexp.MustCompile(`(?ms)^import\s*\(\s*\n.*?\)\s*\n`)
	singleImportRe := regexp.MustCompile(`(?m)^import\s+(\w+\s+)?"[^"]+"\s*\n`)

	if groupedImportRe.MatchString(content) || singleImportRe.MatchString(content) {
		// Add the import and reorganize.
		var imports []ImportEntry
		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			alias := ""
			if imp.Name != nil {
				alias = imp.Name.Name
			}
			imports = append(imports, ImportEntry{Path: path, Alias: alias, Used: true})
		}
		imports = append(imports, ImportEntry{Path: importPath, Used: true})

		modulePath := detectModulePath(content)
		groups := o.groupGoImports(imports, modulePath)
		newBlock := o.FormatImportBlock(groups, "go")
		return replaceGoImportBlock(content, newBlock), nil
	}

	// No import block exists; create one after the package declaration.
	pkgRe := regexp.MustCompile(`(?m)^package\s+\w+\s*\n`)
	if loc := pkgRe.FindStringIndex(content); loc != nil {
		newImport := fmt.Sprintf("\nimport %q\n", importPath)
		return content[:loc[1]] + newImport + content[loc[1]:], nil
	}

	return "", fmt.Errorf("could not find package declaration")
}

func (o *ImportOrganizer) removeGoImport(content, importPath string) (string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", content, parser.ImportsOnly)
	if err != nil {
		return "", fmt.Errorf("parse error: %w", err)
	}

	var imports []ImportEntry
	found := false
	for _, imp := range f.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		if path == importPath {
			found = true
			continue
		}
		alias := ""
		if imp.Name != nil {
			alias = imp.Name.Name
		}
		imports = append(imports, ImportEntry{Path: path, Alias: alias, Used: true})
	}

	if !found {
		return content, nil
	}

	if len(imports) == 0 {
		// Remove the entire import block.
		groupedImportRe := regexp.MustCompile(`(?ms)^import\s*\(\s*\n.*?\)\s*\n`)
		singleImportRe := regexp.MustCompile(`(?m)^import\s+(\w+\s+)?"[^"]+"\s*\n`)
		if loc := groupedImportRe.FindStringIndex(content); loc != nil {
			return content[:loc[0]] + content[loc[1]:], nil
		}
		if loc := singleImportRe.FindStringIndex(content); loc != nil {
			return content[:loc[0]] + content[loc[1]:], nil
		}
		return content, nil
	}

	modulePath := detectModulePath(content)
	groups := o.groupGoImports(imports, modulePath)
	newBlock := o.FormatImportBlock(groups, "go")
	return replaceGoImportBlock(content, newBlock), nil
}

// --- TypeScript-specific helpers ---

type tsImportRegion struct {
	start int
	end   int
}

// tsImportRe matches TypeScript import statements.
var tsImportRe = regexp.MustCompile(`(?m)^import\s+(?:type\s+)?(?:(\{[^}]*\}|[*]\s+as\s+\w+|\w+)(?:\s*,\s*(\{[^}]*\}))?)\s+from\s+['"]([^'"]+)['"]\s*;?\s*$|^import\s+['"]([^'"]+)['"]\s*;?\s*$`)

func parseTypeScriptImports(content string) ([]ImportEntry, tsImportRegion) {
	matches := tsImportRe.FindAllStringSubmatchIndex(content, -1)
	if len(matches) == 0 {
		return nil, tsImportRegion{}
	}

	var imports []ImportEntry
	region := tsImportRegion{
		start: matches[0][0],
		end:   matches[len(matches)-1][1],
	}

	lines := strings.Split(content, "\n")
	importLineRe := regexp.MustCompile(`^\s*import\s+(.+)$`)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !importLineRe.MatchString(line) {
			continue
		}

		entry := parseSingleTSImport(line)
		if entry.Path != "" {
			imports = append(imports, entry)
		}
	}

	return imports, region
}

func parseSingleTSImport(line string) ImportEntry {
	// Side-effect import: import "path";
	sideEffectRe := regexp.MustCompile(`^import\s+['"]([^'"]+)['"]\s*;?\s*$`)
	if m := sideEffectRe.FindStringSubmatch(line); m != nil {
		return ImportEntry{Path: m[1], Alias: "", Used: true}
	}

	// Type import: import type { ... } from "path";
	typeImportRe := regexp.MustCompile(`^import\s+type\s+(.+?)\s+from\s+['"]([^'"]+)['"]\s*;?\s*$`)
	if m := typeImportRe.FindStringSubmatch(line); m != nil {
		return ImportEntry{Path: m[2], Alias: "type:" + m[1], Used: true}
	}

	// Standard import: import { ... } from "path"; or import X from "path";
	standardImportRe := regexp.MustCompile(`^import\s+(.+?)\s+from\s+['"]([^'"]+)['"]\s*;?\s*$`)
	if m := standardImportRe.FindStringSubmatch(line); m != nil {
		return ImportEntry{Path: m[2], Alias: m[1], Used: true}
	}

	return ImportEntry{}
}

func parseTSImportNames(alias string) []string {
	// Handle type: prefix.
	alias = strings.TrimPrefix(alias, "type:")

	// Handle destructured imports: { A, B, C }
	if strings.HasPrefix(alias, "{") {
		inner := strings.Trim(alias, "{} ")
		parts := strings.Split(inner, ",")
		var names []string
		for _, p := range parts {
			p = strings.TrimSpace(p)
			// Handle "X as Y" aliases.
			if idx := strings.Index(p, " as "); idx >= 0 {
				p = strings.TrimSpace(p[idx+4:])
			}
			if p != "" {
				names = append(names, p)
			}
		}
		return names
	}

	// Handle namespace import: * as Name
	if strings.HasPrefix(alias, "* as ") {
		return []string{strings.TrimPrefix(alias, "* as ")}
	}

	// Handle "Default, { Named }" pattern.
	if idx := strings.Index(alias, ","); idx >= 0 {
		defaultName := strings.TrimSpace(alias[:idx])
		rest := strings.TrimSpace(alias[idx+1:])
		names := []string{defaultName}
		names = append(names, parseTSImportNames(rest)...)
		return names
	}

	// Simple default import.
	return []string{strings.TrimSpace(alias)}
}

func getTSBody(content string, imports []ImportEntry) string {
	// Find the last import line and return everything after.
	lines := strings.Split(content, "\n")
	importLineRe := regexp.MustCompile(`^\s*import\s+`)
	lastImportLine := -1
	for i, line := range lines {
		if importLineRe.MatchString(line) {
			lastImportLine = i
		}
	}
	if lastImportLine < 0 {
		return content
	}
	if lastImportLine+1 >= len(lines) {
		return ""
	}
	return strings.Join(lines[lastImportLine+1:], "\n")
}

func (o *ImportOrganizer) groupTSImports(imports []ImportEntry) []ImportGroup {
	builtinGroup := ImportGroup{Name: "builtin"}
	externalGroup := ImportGroup{Name: "external"}
	internalGroup := ImportGroup{Name: "internal"}

	for _, imp := range imports {
		if strings.HasPrefix(imp.Path, "node:") {
			builtinGroup.Imports = append(builtinGroup.Imports, imp)
		} else if strings.HasPrefix(imp.Path, ".") || strings.HasPrefix(imp.Path, "..") {
			internalGroup.Imports = append(internalGroup.Imports, imp)
		} else {
			externalGroup.Imports = append(externalGroup.Imports, imp)
		}
	}

	// Sort within groups.
	sortImports := func(imps []ImportEntry) {
		sort.Slice(imps, func(i, j int) bool {
			return imps[i].Path < imps[j].Path
		})
	}
	sortImports(builtinGroup.Imports)
	sortImports(externalGroup.Imports)
	sortImports(internalGroup.Imports)

	var groups []ImportGroup
	if len(builtinGroup.Imports) > 0 {
		groups = append(groups, builtinGroup)
	}
	if len(externalGroup.Imports) > 0 {
		groups = append(groups, externalGroup)
	}
	if len(internalGroup.Imports) > 0 {
		groups = append(groups, internalGroup)
	}
	return groups
}

func formatTSImportBlock(groups []ImportGroup) string {
	if len(groups) == 0 {
		return ""
	}

	var b strings.Builder
	for i, group := range groups {
		for _, imp := range group.Imports {
			if imp.Alias == "" {
				// Side-effect import.
				b.WriteString(fmt.Sprintf("import '%s';\n", imp.Path))
			} else if strings.HasPrefix(imp.Alias, "type:") {
				// Type import.
				names := strings.TrimPrefix(imp.Alias, "type:")
				b.WriteString(fmt.Sprintf("import type %s from '%s';\n", names, imp.Path))
			} else {
				// Standard import.
				b.WriteString(fmt.Sprintf("import %s from '%s';\n", imp.Alias, imp.Path))
			}
		}
		// Add blank line between groups.
		if i < len(groups)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func (o *ImportOrganizer) addMissingTSImport(content, importPath string) (string, error) {
	// Check if already imported.
	imports, _ := parseTypeScriptImports(content)
	for _, imp := range imports {
		if imp.Path == importPath {
			return content, nil
		}
	}

	// Determine where the import should go and add it.
	newImport := fmt.Sprintf("import '%s';\n", importPath)

	importLineRe := regexp.MustCompile(`(?m)^\s*import\s+`)
	if loc := importLineRe.FindStringIndex(content); loc != nil {
		// Add at the end of existing imports and reorganize.
		return content[:loc[0]] + newImport + content[loc[0]:], nil
	}

	// No existing imports; add at the top.
	return newImport + "\n" + content, nil
}

func (o *ImportOrganizer) removeTSImport(content, importPath string) (string, error) {
	// Match import line with this specific path.
	escaped := regexp.QuoteMeta(importPath)
	re := regexp.MustCompile(`(?m)^import\s+.*?['"]` + escaped + `['"]\s*;?\s*\n?`)
	if !re.MatchString(content) {
		return content, nil
	}
	result := re.ReplaceAllString(content, "")
	return result, nil
}

// --- ImportOrganizerTool implements the Tool interface ---

// ImportOrganizerTool organizes imports in Go and TypeScript files.
type ImportOrganizerTool struct{}

func (ImportOrganizerTool) Name() string { return "OrganizeImports" }
func (ImportOrganizerTool) Description() string {
	return "Organize and fix imports in Go and TypeScript files. Groups imports by category, sorts alphabetically, and removes unused imports."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (ImportOrganizerTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"path": {Type: "string", Description: "Absolute path to the file to organize imports in"},
		},
		Required: []string{"path"},
	}
}

func (ImportOrganizerTool) Parameters() map[string]interface{} {
	return importOrganizerSchema.ToJSONSchema()
}

// importOrganizerSchema is the single source of truth for ImportOrganizer's input schema.
var importOrganizerSchema = ImportOrganizerTool{}.Schema()

// ImportOrganizerInput is the typed input for ImportOrganizerTool.
type ImportOrganizerInput struct {
	Path string `json:"path"`
}

func (ImportOrganizerTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[ImportOrganizerInput]("ImportOrganizer", input)
	if err != nil {
		return "", err
	}
	if p.Path == "" {
		return "", fmt.Errorf("path is required")
	}

	if err := validatePathAllowed(ctx, p.Path); err != nil {
		return "", err
	}

	// Read the file through a directory-pinned root so a symlink swap cannot
	// redirect the operation after the policy check.
	data, err := readGuardedFile(ctx, p.Path)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}
	content := string(data)

	// Detect language from extension.
	var language string
	if strings.HasSuffix(p.Path, ".go") {
		language = "go"
	} else if strings.HasSuffix(p.Path, ".ts") || strings.HasSuffix(p.Path, ".tsx") {
		language = "typescript"
	} else {
		return "", fmt.Errorf("unsupported file type: %s", p.Path)
	}

	organizer := NewImportOrganizer(language)
	var result string

	switch language {
	case "go":
		result, err = organizer.OrganizeGo(content)
	case "typescript":
		result, err = organizer.OrganizeTypeScript(content)
	}
	if err != nil {
		return "", fmt.Errorf("organize imports: %w", err)
	}

	// Write back.
	if err := writeGuardedFile(ctx, p.Path, []byte(result), 0o600); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}

	return fmt.Sprintf("Organized imports in %s", p.Path), nil
}

// Ensure ImportOrganizerTool satisfies Tool interface at compile time.
var _ Tool = ImportOrganizerTool{}

// Suppress unused import warnings for packages used only in ast walking.
var (
	_ = (*ast.File)(nil)
	_ = token.NoPos
)
