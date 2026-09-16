package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/GrayCodeAI/rho/internal/codegraph"
)

// CodeGraphTool provides tree-sitter based code intelligence.
type CodeGraphTool struct{}

// CodeGraphInput is the typed input for CodeGraphTool.
type CodeGraphInput struct {
	Action   string `json:"action"`
	Query    string `json:"query"`
	NodeID   string `json:"node_id"`
	MaxDepth int    `json:"max_depth"`
	MaxNodes int    `json:"max_nodes"`
	Root     string `json:"root"`
	From     string `json:"from"`
	To       string `json:"to"`
	MaxFiles int    `json:"max_files"`
	Dir      string `json:"dir"`
}

func (CodeGraphTool) Name() string      { return "CodeGraph" }
func (CodeGraphTool) RiskLevel() string { return "low" }
func (CodeGraphTool) Aliases() []string { return []string{"cg", "graph"} }
func (CodeGraphTool) Description() string {
	return "Query the code knowledge graph: search symbols, trace callers/callees, compute impact radius, build context."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (CodeGraphTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"action":    {Type: "string", Enum: []interface{}{"search", "callers", "callees", "impact", "context", "index", "sync", "trace", "explore", "files", "status", "stats", "pagerank", "centrality", "communities", "components", "deadcode", "coupling", "cross_repo", "semantic_search", "hybrid_search"}, Description: "Action: search/find symbols, callers/who calls, callees/what it calls, impact/breakage radius, context/build task context, index/full re-index, sync/incremental update, trace/call path A→B, explore/multi-symbol source, files/list indexed, status/health check, stats/counts, pagerank/file importance, centrality/bridge files, communities/module clusters, components/isolated subsystems, deadcode/unused code, coupling/tightly coupled files, cross_repo/cross-repo dependencies, semantic_search/embedding-based search, hybrid_search/combined FTS5+semantic"},
			"query":     {Type: "string", Description: "Search query, symbol name, or task description (for context/trace use 'from -> to')"},
			"node_id":   {Type: "string", Description: "Node ID for callers/callees/impact"},
			"max_depth": {Type: "integer", Description: "Max traversal depth (default: 3)"},
			"max_nodes": {Type: "integer", Description: "Max nodes to return (default: 30)"},
			"root":      {Type: "string", Description: "Project root directory (default: current dir)"},
			"from":      {Type: "string", Description: "Source symbol for trace action"},
			"to":        {Type: "string", Description: "Target symbol for trace action"},
			"max_files": {Type: "integer", Description: "Max files for explore action (default: 10)"},
			"dir":       {Type: "string", Description: "Directory filter for files action"},
		},
		Required: []string{"action"},
	}
}

func (CodeGraphTool) Parameters() map[string]interface{} {
	return codeGraphSchema.ToJSONSchema()
}

// codeGraphSchema is the single source of truth for CodeGraph's input schema.
var codeGraphSchema = CodeGraphTool{}.Schema()

func (CodeGraphTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[CodeGraphInput]("CodeGraph", input)
	if err != nil {
		return "", err
	}

	root := p.Root
	if root == "" {
		root = "."
	}
	if err := validatePathAllowed(ctx, root); err != nil {
		return "", err
	}
	if p.MaxDepth <= 0 {
		p.MaxDepth = 3
	}
	if p.MaxNodes <= 0 {
		p.MaxNodes = 30
	}
	if p.MaxFiles <= 0 {
		p.MaxFiles = 10
	}

	cg, err := codegraph.Open(root)
	if err != nil {
		return "", fmt.Errorf("codegraph: %w", err)
	}
	defer func() { _ = cg.Close() }()

	switch p.Action {
	case "index":
		return indexCodeGraph(cg, root)
	case "sync":
		return syncCodeGraph(cg)
	case "search":
		return searchCodeGraph(cg, p.Query, p.MaxNodes)
	case "callers":
		return callersCodeGraph(cg, p.NodeID, p.MaxDepth)
	case "callees":
		return calleesCodeGraph(cg, p.NodeID, p.MaxDepth)
	case "impact":
		return impactCodeGraph(cg, p.NodeID, p.MaxDepth)
	case "context":
		return contextCodeGraph(cg, p.Query, p.MaxNodes)
	case "trace":
		return traceCodeGraph(cg, p.From, p.To)
	case "explore":
		return exploreCodeGraph(cg, p.Query, p.MaxFiles)
	case "files":
		return filesCodeGraph(cg, p.Dir)
	case "status":
		return statusCodeGraph(cg)
	case "stats":
		return statsCodeGraph(cg)
	case "pagerank":
		return pagerankCodeGraph(cg, p.MaxNodes)
	case "centrality":
		return centralityCodeGraph(cg, p.MaxNodes)
	case "communities":
		return communitiesCodeGraph(cg)
	case "components":
		return componentsCodeGraph(cg)
	case "deadcode":
		return deadcodeCodeGraph(cg)
	case "coupling":
		return couplingCodeGraph(cg, p.MaxNodes)
	case "cross_repo":
		return crossRepoCodeGraph(p.Query, p.MaxNodes)
	case "semantic_search":
		return semanticSearchCodeGraph(cg, p.Query, p.MaxNodes)
	case "hybrid_search":
		return hybridSearchCodeGraph(cg, p.Query, p.MaxNodes)
	default:
		return "", fmt.Errorf("unknown action: %s", p.Action)
	}
}

func indexCodeGraph(cg *codegraph.CodeGraph, root string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}

	// Check if already indexed
	dbPath := filepath.Join(absRoot, ".codegraph", "codegraph.db")
	if _, err := os.Stat(dbPath); err == nil {
		// Already indexed, do incremental sync
		return "CodeGraph index already exists. Use 'search' or 'context' to query it.", nil
	}

	if err := cg.IndexDir(absRoot); err != nil {
		return "", fmt.Errorf("indexing failed: %w", err)
	}

	stats, _ := cg.Stats()
	return fmt.Sprintf("Indexed %d files, %d symbols, %d edges", stats["files"], stats["nodes"], stats["edges"]), nil
}

func searchCodeGraph(cg *codegraph.CodeGraph, query string, limit int) (string, error) {
	if query == "" {
		return "", fmt.Errorf("query is required for search")
	}

	nodes, err := cg.Search(query, limit)
	if err != nil {
		return "", err
	}

	if len(nodes) == 0 {
		return fmt.Sprintf("No symbols found for '%s'", query), nil
	}

	var result string
	result += fmt.Sprintf("## Search Results for '%s' (%d found)\n\n", query, len(nodes))
	for _, n := range nodes {
		sig := n.Signature
		if sig == "" {
			sig = n.QualifiedName
		}
		result += fmt.Sprintf("- **%s** `%s` in %s:%d\n", n.Kind, sig, n.FilePath, n.StartLine)
	}
	return result, nil
}

func callersCodeGraph(cg *codegraph.CodeGraph, nodeID string, depth int) (string, error) {
	if nodeID == "" {
		return "", fmt.Errorf("node_id is required for callers")
	}

	nodes, err := cg.GetCallers(nodeID, depth)
	if err != nil {
		return "", err
	}

	if len(nodes) == 0 {
		return "No callers found", nil
	}

	var result string
	result += fmt.Sprintf("## Callers (%d found)\n\n", len(nodes))
	for _, n := range nodes {
		result += fmt.Sprintf("- **%s** `%s` in %s:%d\n", n.Kind, n.QualifiedName, n.FilePath, n.StartLine)
	}
	return result, nil
}

func calleesCodeGraph(cg *codegraph.CodeGraph, nodeID string, depth int) (string, error) {
	if nodeID == "" {
		return "", fmt.Errorf("node_id is required for callees")
	}

	nodes, err := cg.GetCallees(nodeID, depth)
	if err != nil {
		return "", err
	}

	if len(nodes) == 0 {
		return "No callees found", nil
	}

	var result string
	result += fmt.Sprintf("## Callees (%d found)\n\n", len(nodes))
	for _, n := range nodes {
		result += fmt.Sprintf("- **%s** `%s` in %s:%d\n", n.Kind, n.QualifiedName, n.FilePath, n.StartLine)
	}
	return result, nil
}

func impactCodeGraph(cg *codegraph.CodeGraph, nodeID string, depth int) (string, error) {
	if nodeID == "" {
		return "", fmt.Errorf("node_id is required for impact")
	}

	nodes, err := cg.GetImpactRadius(nodeID, depth)
	if err != nil {
		return "", err
	}

	if len(nodes) == 0 {
		return "No impact found", nil
	}

	var result string
	result += fmt.Sprintf("## Impact Radius (%d nodes affected)\n\n", len(nodes))
	for _, n := range nodes {
		result += fmt.Sprintf("- **%s** `%s` in %s:%d\n", n.Kind, n.QualifiedName, n.FilePath, n.StartLine)
	}
	return result, nil
}

func contextCodeGraph(cg *codegraph.CodeGraph, query string, maxNodes int) (string, error) {
	if query == "" {
		return "", fmt.Errorf("query is required for context")
	}

	return cg.BuildContext(query, maxNodes)
}

func statsCodeGraph(cg *codegraph.CodeGraph) (string, error) {
	stats, err := cg.Stats()
	if err != nil {
		return "", err
	}

	var result string
	result += "## CodeGraph Statistics\n\n"
	result += fmt.Sprintf("- Files: %d\n", stats["files"])
	result += fmt.Sprintf("- Nodes: %d\n", stats["nodes"])
	result += fmt.Sprintf("- Edges: %d\n", stats["edges"])

	if byKind, ok := stats["nodes_by_kind"].(map[string]int); ok {
		result += "\n### Nodes by Kind\n"
		for kind, count := range byKind {
			result += fmt.Sprintf("- %s: %d\n", kind, count)
		}
	}

	return result, nil
}

func syncCodeGraph(cg *codegraph.CodeGraph) (string, error) {
	result, err := cg.Sync()
	if err != nil {
		return "", fmt.Errorf("sync failed: %w", err)
	}

	var msg string
	msg += "## Sync Complete\n\n"
	msg += fmt.Sprintf("- Files checked: %d\n", result.FilesChecked)
	msg += fmt.Sprintf("- Added: %d\n", result.FilesAdded)
	msg += fmt.Sprintf("- Modified: %d\n", result.FilesModified)
	msg += fmt.Sprintf("- Removed: %d\n", result.FilesRemoved)
	msg += fmt.Sprintf("- Nodes updated: %d\n", result.NodesUpdated)
	msg += fmt.Sprintf("- Duration: %dms\n", result.DurationMs)

	if result.FilesAdded == 0 && result.FilesModified == 0 && result.FilesRemoved == 0 {
		msg += "\nIndex is up to date."
	}

	return msg, nil
}

func traceCodeGraph(cg *codegraph.CodeGraph, from, to string) (string, error) {
	if from == "" || to == "" {
		return "", fmt.Errorf("both 'from' and 'to' are required for trace")
	}

	path, err := cg.Trace(from, to)
	if err != nil {
		return fmt.Sprintf("No call path found from %q to %q: %v", from, to, err), nil
	}

	var result string
	result += fmt.Sprintf("## Trace: %s → %s\n\n", from, to)
	result += fmt.Sprintf("%d hops:\n\n", len(path))

	for i, n := range path {
		sig := n.QualifiedName
		if n.Signature != "" {
			sig = n.Signature
		}
		if i == 0 {
			result += fmt.Sprintf("  **START** %s `%s` in %s:%d\n", n.Kind, sig, n.FilePath, n.StartLine)
		} else if i == len(path)-1 {
			result += fmt.Sprintf("  **END** %s `%s` in %s:%d\n", n.Kind, sig, n.FilePath, n.StartLine)
		} else {
			result += fmt.Sprintf("  → %s `%s` in %s:%d\n", n.Kind, sig, n.FilePath, n.StartLine)
		}
	}

	return result, nil
}

func exploreCodeGraph(cg *codegraph.CodeGraph, query string, maxFiles int) (string, error) {
	if query == "" {
		return "", fmt.Errorf("query is required for explore")
	}

	result, err := cg.Explore(query, maxFiles)
	if err != nil {
		return "", err
	}

	var msg string
	msg += fmt.Sprintf("## Explore: %s\n\n", query)

	for filePath, nodes := range result.Files {
		msg += fmt.Sprintf("### %s\n", filePath)
		for _, n := range nodes {
			sig := n.QualifiedName
			if n.Signature != "" {
				sig = n.Signature
			}
			msg += fmt.Sprintf("- **%s** `%s` (line %d)\n", n.Kind, sig, n.StartLine)

			key := fmt.Sprintf("%s:%d", filePath, n.StartLine)
			if source, ok := result.SourceLines[key]; ok {
				msg += fmt.Sprintf("  ```\n  %s\n  ```\n", truncateLines(source, 20))
			}
		}
		msg += "\n"
	}

	return msg, nil
}

func filesCodeGraph(cg *codegraph.CodeGraph, dir string) (string, error) {
	files, err := cg.Files(dir)
	if err != nil {
		return "", err
	}

	var msg string
	msg += "## Indexed Files\n\n"
	if len(files) == 0 {
		msg += "No files indexed. Run 'index' first.\n"
		return msg, nil
	}

	msg += fmt.Sprintf("%d files indexed:\n\n", len(files))
	for _, f := range files {
		msg += fmt.Sprintf("- `%s` [%s] %d symbols\n", f.Path, f.Language, f.NodeCount)
	}

	return msg, nil
}

func statusCodeGraph(cg *codegraph.CodeGraph) (string, error) {
	status, err := cg.Status()
	if err != nil {
		return "", err
	}

	var msg string
	msg += "## CodeGraph Status\n\n"
	msg += fmt.Sprintf("- Project: %s\n", status.ProjectRoot)
	msg += fmt.Sprintf("- DB: %s (%.2f MB)\n", status.DBPath, float64(status.DBSizeBytes)/1024/1024)
	msg += fmt.Sprintf("- Journal: %s\n", status.JournalMode)
	msg += fmt.Sprintf("- Up to date: %v\n", status.UpToDate)
	msg += "\n"
	msg += fmt.Sprintf("- Files: %d\n", status.Files)
	msg += fmt.Sprintf("- Nodes: %d\n", status.Nodes)
	msg += fmt.Sprintf("- Edges: %d\n", status.Edges)
	msg += fmt.Sprintf("- Unresolved refs: %d\n", status.Unresolved)

	if len(status.NodesByKind) > 0 {
		msg += "\n### Nodes by Kind\n"
		for kind, count := range status.NodesByKind {
			msg += fmt.Sprintf("- %s: %d\n", kind, count)
		}
	}

	if len(status.FilesByLang) > 0 {
		msg += "\n### Files by Language\n"
		for lang, count := range status.FilesByLang {
			msg += fmt.Sprintf("- %s: %d\n", lang, count)
		}
	}

	return msg, nil
}

func semanticSearchCodeGraph(cg *codegraph.CodeGraph, query string, limit int) (string, error) {
	if query == "" {
		return "", fmt.Errorf("query is required for semantic_search")
	}

	nodes, err := cg.SemanticSearch(query, limit)
	if err != nil {
		return "", err
	}

	if len(nodes) == 0 {
		return fmt.Sprintf("No semantic matches found for %q", query), nil
	}

	var msg string
	msg += fmt.Sprintf("## Semantic Search: %q\n\n", query)
	msg += fmt.Sprintf("Found %d matches (embedding-based similarity):\n\n", len(nodes))

	for i, n := range nodes {
		sig := n.QualifiedName
		if n.Signature != "" {
			sig = n.Signature
		}
		msg += fmt.Sprintf("%d. **%s** `%s` in %s:%d\n", i+1, n.Kind, sig, n.FilePath, n.StartLine)
	}

	return msg, nil
}

func hybridSearchCodeGraph(cg *codegraph.CodeGraph, query string, limit int) (string, error) {
	if query == "" {
		return "", fmt.Errorf("query is required for hybrid_search")
	}

	nodes, err := cg.HybridSearch(query, limit)
	if err != nil {
		return "", err
	}

	if len(nodes) == 0 {
		return fmt.Sprintf("No matches found for %q", query), nil
	}

	var msg string
	msg += fmt.Sprintf("## Hybrid Search: %q\n\n", query)
	msg += fmt.Sprintf("Found %d matches (FTS5 + semantic fusion):\n\n", len(nodes))

	for i, n := range nodes {
		sig := n.QualifiedName
		if n.Signature != "" {
			sig = n.Signature
		}
		msg += fmt.Sprintf("%d. **%s** `%s` in %s:%d\n", i+1, n.Kind, sig, n.FilePath, n.StartLine)
	}

	return msg, nil
}

func truncateLines(s string, maxLines int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= maxLines {
		return s
	}
	return strings.Join(lines[:maxLines], "\n") + "\n..."
}

func pagerankCodeGraph(cg *codegraph.CodeGraph, topN int) (string, error) {
	ranks, err := cg.PageRank(20, 0.85)
	if err != nil {
		return "", err
	}

	// Sort by rank
	type scored struct {
		id    string
		score float64
	}
	var sorted []scored
	for id, score := range ranks {
		sorted = append(sorted, scored{id, score})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].score > sorted[j].score
	})

	if topN <= 0 {
		topN = 20
	}
	if topN > len(sorted) {
		topN = len(sorted)
	}

	var msg string
	msg += "## PageRank — File/Symbol Importance\n\n"
	msg += fmt.Sprintf("Top %d most important symbols (by call graph centrality):\n\n", topN)

	for i, s := range sorted[:topN] {
		// Load node details
		node, err := cg.GetNode(s.id)
		if err != nil {
			continue
		}
		msg += fmt.Sprintf("%d. **%s** `%s` in %s:%d (rank: %.4f)\n",
			i+1, node.Kind, node.Name, node.FilePath, node.StartLine, s.score)
	}

	return msg, nil
}

func centralityCodeGraph(cg *codegraph.CodeGraph, topN int) (string, error) {
	result, err := cg.BetweennessCentrality(topN)
	if err != nil {
		return "", err
	}

	var msg string
	msg += "## Betweenness Centrality — Bridge Files\n\n"
	msg += "High-centrality nodes are \"bridges\" connecting different parts of the codebase.\n"
	msg += "These are coupling hotspots — changes here have wide ripple effects.\n\n"

	for i, nc := range result.Top {
		msg += fmt.Sprintf("%d. **%s** `%s` in %s (centrality: %.6f)\n",
			i+1, nc.Kind, nc.Name, nc.FilePath, nc.Score)
	}

	return msg, nil
}

func communitiesCodeGraph(cg *codegraph.CodeGraph) (string, error) {
	result, err := cg.CommunityDetection()
	if err != nil {
		return "", err
	}

	var msg string
	msg += "## Community Detection — Module Boundaries\n\n"
	msg += fmt.Sprintf("Found %d communities (modularity: %.4f)\n\n",
		len(result.Communities), result.Modularity)

	for i, comm := range result.Communities {
		if i >= 10 { // Show top 10
			break
		}
		msg += fmt.Sprintf("### Community %d (%d nodes)\n", comm.ID, len(comm.Nodes))
		// Show first 5 nodes
		show := 5
		if show > len(comm.Nodes) {
			show = len(comm.Nodes)
		}
		for _, nodeID := range comm.Nodes[:show] {
			node, err := cg.GetNode(nodeID)
			if err != nil {
				continue
			}
			msg += fmt.Sprintf("- %s `%s` in %s\n", node.Kind, node.Name, node.FilePath)
		}
		if len(comm.Nodes) > 5 {
			msg += fmt.Sprintf("- ... and %d more\n", len(comm.Nodes)-5)
		}
		msg += "\n"
	}

	return msg, nil
}

func componentsCodeGraph(cg *codegraph.CodeGraph) (string, error) {
	components, err := cg.ConnectedComponents()
	if err != nil {
		return "", err
	}

	var msg string
	msg += "## Connected Components — Isolated Subsystems\n\n"
	msg += fmt.Sprintf("Found %d disconnected components:\n\n", len(components))

	for i, comp := range components {
		if i >= 10 {
			break
		}
		msg += fmt.Sprintf("### Component %d (%d nodes)\n", i+1, len(comp))
		show := 5
		if show > len(comp) {
			show = len(comp)
		}
		for _, nodeID := range comp[:show] {
			node, err := cg.GetNode(nodeID)
			if err != nil {
				continue
			}
			msg += fmt.Sprintf("- %s `%s` in %s\n", node.Kind, node.Name, node.FilePath)
		}
		if len(comp) > 5 {
			msg += fmt.Sprintf("- ... and %d more\n", len(comp)-5)
		}
		msg += "\n"
	}

	return msg, nil
}

func deadcodeCodeGraph(cg *codegraph.CodeGraph) (string, error) {
	dead, err := cg.FindDeadCode()
	if err != nil {
		return "", err
	}

	var msg string
	msg += "## Dead Code Detection\n\n"
	msg += fmt.Sprintf("Found %d potentially unused symbols:\n\n", len(dead))

	for i, entry := range dead {
		if i >= 30 {
			break
		}
		msg += fmt.Sprintf("%d. **%s** `%s` in %s:%d (confidence: %.0f%%) — %s\n",
			i+1, entry.Node.Kind, entry.Node.Name, entry.Node.FilePath,
			entry.Node.StartLine, entry.Confidence*100, entry.Reason)
	}

	return msg, nil
}

func couplingCodeGraph(cg *codegraph.CodeGraph, topN int) (string, error) {
	metrics, err := cg.AnalyzeCoupling(topN)
	if err != nil {
		return "", err
	}

	var msg string
	msg += "## Coupling Analysis — Tightly Coupled Files\n\n"
	msg += "Files that share many dependencies are tightly coupled.\n\n"

	for i, m := range metrics {
		msg += fmt.Sprintf("%d. `%s` ↔ `%s` (coupling: %.2f, shared deps: %d)\n",
			i+1, m.FileA, m.FileB, m.Coupling, m.SharedDeps)
	}

	return msg, nil
}

func crossRepoCodeGraph(query string, maxNodes int) (string, error) {
	if query == "" {
		return "", fmt.Errorf("query is required for cross_repo")
	}

	// Auto-discover repos with .codegraph/
	cwd, _ := os.Getwd()
	parentDir := filepath.Dir(cwd)
	var repos []string

	entries, err := os.ReadDir(parentDir)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				dbPath := filepath.Join(parentDir, entry.Name(), ".codegraph", "codegraph.db")
				if _, statErr := os.Stat(dbPath); statErr == nil {
					repos = append(repos, filepath.Join(parentDir, entry.Name()))
				}
			}
		}
	}

	if len(repos) == 0 {
		return "No repos with .codegraph/ found. Run 'codegraph init' in other repos first.", nil
	}

	results, err := codegraph.CrossRepoQuery(repos, query, maxNodes)
	if err != nil {
		return "", err
	}

	var msg string
	msg += fmt.Sprintf("## Cross-Repo Search: %q\n\n", query)
	msg += fmt.Sprintf("Searched %d repos:\n\n", len(repos))

	for repo, nodes := range results {
		repoName := filepath.Base(repo)
		msg += fmt.Sprintf("### %s (%d matches)\n", repoName, len(nodes))
		for _, n := range nodes {
			sig := n.QualifiedName
			if n.Signature != "" {
				sig = n.Signature
			}
			msg += fmt.Sprintf("- **%s** `%s` in %s:%d\n", n.Kind, sig, n.FilePath, n.StartLine)
		}
		msg += "\n"
	}

	return msg, nil
}
