package permissions

import "strings"

// CanonicalToolName is the shared tool-name normalizer used by every policy
// backend. Wire aliases must resolve to the same policy identity as their
// built-in tool; otherwise a grant can be visible in one layer and missed in
// another.
func CanonicalToolName(name string) string {
	raw := strings.TrimSpace(name)
	switch strings.ToLower(raw) {
	case "bash":
		return "Bash"
	case "powershell", "power_shell":
		return "PowerShell"
	case "file_read", "read":
		return "Read"
	case "file_write", "write":
		return "Write"
	case "file_edit", "edit":
		return "Edit"
	case "ls":
		return "LS"
	case "glob":
		return "Glob"
	case "grep":
		return "Grep"
	case "web_fetch", "webfetch":
		return "WebFetch"
	case "web_search", "websearch":
		return "WebSearch"
	case "code_match", "codematch", "match_code":
		return "CodeMatch"
	case "fuzzy_find", "fuzzyfind", "ffind":
		return "FuzzyFind"
	case "batch_exec", "batchexec":
		return "BatchExec"
	case "toolset":
		return "Toolset"
	case "tool_search", "toolsearch":
		return "ToolSearch"
	case "tool_health", "toolhealth", "tools_health":
		return "ToolHealth"
	case "project_verify", "projectverify", "verify_project":
		return "ProjectVerify"
	case "app_verify", "appverify", "verify_app":
		return "AppVerify"
	case "generate_media", "generatemedia", "media":
		return "GenerateMedia"
	case "dependency_audit", "dependencyaudit", "deps":
		return "DependencyAudit"
	case "git_history", "githistory", "git-history":
		return "GitHistory"
	case "github", "gh":
		return "GitHub"
	case "sql", "sql_query":
		return "SQL"
	case "agent", "task":
		return "Agent"
	case "ask_user", "askuser", "askuserquestion":
		return "AskUserQuestion"
	case "todo", "todowrite":
		return "TodoWrite"
	case "lsp":
		return "LSP"
	case "specify":
		return "Specify"
	case "plan":
		return "Plan"
	case "tasks":
		return "Tasks"
	case "approve_implementation", "approveimplementation":
		return "ApproveImplementation"
	case "spec_status", "specstatus":
		return "SpecStatus"
	case "spec_edit", "specedit":
		return "SpecEdit"
	case "spec_list", "speclist":
		return "SpecList"
	case "spec_reset", "specreset":
		return "SpecReset"
	case "spec_config", "specconfig":
		return "SpecConfig"
	case "clarify":
		return "Clarify"
	case "analyze":
		return "Analyze"
	case "checklist":
		return "Checklist"
	case "constitution":
		return "Constitution"
	case "converge":
		return "Converge"
	case "notebook_edit", "notebookedit":
		return "NotebookEdit"
	case "terminal_create", "terminalcreate", "pty_create":
		return "TerminalCreate"
	case "terminal_send", "terminalsend", "pty_send":
		return "TerminalSend"
	case "config":
		return "Config"
	case "brief", "sendusermessage":
		return "SendUserMessage"
	default:
		return raw
	}
}
