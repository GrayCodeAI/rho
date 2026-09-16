package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// GitHubTool exposes bounded, read-only GitHub inspection through the
// authenticated gh CLI. Mutating operations intentionally remain separate
// from this tool so a model cannot create, merge, or comment by accident.
type GitHubTool struct{}

func (GitHubTool) Name() string      { return "GitHub" }
func (GitHubTool) RiskLevel() string { return "medium" }
func (GitHubTool) Aliases() []string { return []string{"github", "gh"} }
func (GitHubTool) Description() string {
	return "Inspect GitHub repositories, pull requests, issues, checks, and workflow runs through the authenticated gh CLI. Read-only; creating, merging, commenting, and pushing require explicit Git/Bash workflows."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (GitHubTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"action": {Type: "string", Enum: []interface{}{"auth_status", "repo", "pr_list", "pr_view", "pr_diff", "pr_checks", "issue_list", "issue_view", "run_list"}},
			"ref":    {Type: "string", Description: "PR, issue, or workflow reference (number, URL, or branch where supported)."},
			"limit":  {Type: "integer", Minimum: 1, Maximum: 50, Description: "Maximum records for list actions (default 20)."},
			"path":   {Type: "string", Description: "Repository working directory (default: session working directory)."},
		},
		Required: []string{"action"},
	}
}

func (GitHubTool) Parameters() map[string]interface{} {
	return gitHubSchema.ToJSONSchema()
}

// gitHubSchema is the single source of truth for GitHub's input schema.
var gitHubSchema = GitHubTool{}.Schema()

// GitHubInput is the typed input for GitHubTool.
type GitHubInput struct {
	Action string `json:"action"`
	Ref    string `json:"ref"`
	Limit  int    `json:"limit"`
	Path   string `json:"path"`
}

func (GitHubTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	params, err := DecodeInput[GitHubInput]("GitHub", input)
	if err != nil {
		return "", err
	}
	params.Action = strings.ToLower(strings.TrimSpace(params.Action))
	if params.Limit <= 0 {
		params.Limit = 20
	}
	if params.Limit > 50 {
		params.Limit = 50
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return "", fmt.Errorf("GitHub CLI (gh) is not installed or not on PATH")
	}

	root := params.Path
	if root == "" {
		if tc := GetToolContext(ctx); tc != nil && tc.WorkingDir != "" {
			root = tc.WorkingDir
		} else {
			root, _ = os.Getwd()
		}
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve repository path: %w", err)
	}
	if err := validatePathAllowed(ctx, root); err != nil {
		return "", err
	}

	args, err := githubArgs(params.Action, params.Ref, params.Limit)
	if err != nil {
		return "", err
	}
	command := "gh " + strings.Join(args, " ")
	cmdCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	// #nosec G204 -- gh is a fixed executable and githubArgs only returns
	// allowlisted subcommands/flags; user input is limited to a validated ref.
	cmd := exec.CommandContext(cmdCtx, "gh", args...)
	cmd.Dir = root
	out, execErr := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if len(text) > 200_000 {
		text = text[:200_000] + "\n[output truncated]"
	}
	if cmdCtx.Err() != nil {
		return "", fmt.Errorf("%s: %w", command, cmdCtx.Err())
	}
	if execErr != nil {
		if text == "" {
			text = execErr.Error()
		}
		return "", fmt.Errorf("%s failed: %s", command, text)
	}
	return text, nil
}

func githubArgs(action, ref string, limit int) ([]string, error) {
	jsonFields := func(fields string) []string { return []string{"--json", fields} }
	switch action {
	case "auth_status":
		return []string{"auth", "status"}, nil
	case "repo":
		return append([]string{"repo", "view"}, jsonFields("nameWithOwner,description,defaultBranchRef,url")...), nil
	case "pr_list":
		return append([]string{"pr", "list", "--limit", fmt.Sprint(limit)}, jsonFields("number,title,state,url,author,headRefName,baseRefName")...), nil
	case "pr_view":
		if strings.TrimSpace(ref) == "" {
			return nil, fmt.Errorf("ref is required for pr_view")
		}
		return append([]string{"pr", "view", ref}, jsonFields("number,title,body,state,author,assignees,labels,reviews,comments,commits,files,url")...), nil
	case "pr_diff":
		if strings.TrimSpace(ref) == "" {
			return nil, fmt.Errorf("ref is required for pr_diff")
		}
		return []string{"pr", "diff", ref}, nil
	case "pr_checks":
		if strings.TrimSpace(ref) == "" {
			return nil, fmt.Errorf("ref is required for pr_checks")
		}
		return append([]string{"pr", "checks", ref}, jsonFields("name,state,bucket,link")...), nil
	case "issue_list":
		return append([]string{"issue", "list", "--limit", fmt.Sprint(limit)}, jsonFields("number,title,state,url,author,labels")...), nil
	case "issue_view":
		if strings.TrimSpace(ref) == "" {
			return nil, fmt.Errorf("ref is required for issue_view")
		}
		return append([]string{"issue", "view", ref}, jsonFields("number,title,body,state,author,assignees,labels,comments,url")...), nil
	case "run_list":
		return append([]string{"run", "list", "--limit", fmt.Sprint(limit)}, jsonFields("databaseId,status,conclusion,name,url,headBranch,createdAt")...), nil
	default:
		return nil, fmt.Errorf("unsupported GitHub action %q", action)
	}
}
