package errs

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
)

type ErrorContext struct {
	Patterns map[string]*ErrorHelp
	mu       sync.RWMutex
}

type ErrorHelp struct {
	Pattern     *regexp.Regexp
	Title       string
	Explanation string
	Suggestions []string
	Examples    []string
	DocURL      string
	AutoFix     string
}

type EnrichedError struct {
	Original    string
	Title       string
	Explanation string
	Suggestions []string
	Examples    []string
	Severity    string
	Recoverable bool
}

func NewErrorContext() *ErrorContext {
	ec := &ErrorContext{
		Patterns: make(map[string]*ErrorHelp, len(builtinErrorPatterns)),
	}
	for key, help := range builtinErrorPatterns {
		ec.Patterns[key] = help
	}
	return ec
}

// builtinErrorPatterns is the static error-pattern table consulted by
// ErrorContext. Keeping it as data (rather than a 500-line constructor) makes
// it reviewable and keeps NewErrorContext small.
var builtinErrorPatterns = map[string]*ErrorHelp{
	"go_undefined": {
		Pattern:     regexp.MustCompile(`undefined:\s*(\w+)`),
		Title:       "Undefined identifier",
		Explanation: "The identifier is used but has not been declared in the current scope. This can happen when a variable, function, or type is misspelled, not imported, or declared in a different scope.",
		Suggestions: []string{
			"Check for typos in the identifier name",
			"Ensure the package is imported if it belongs to another package",
			"Verify the variable is declared before use",
			"Check if the identifier is exported (starts with uppercase)",
		},
		Examples: []string{
			"var myVar int // declare before use",
			"import \"fmt\" // import the needed package",
		},
		DocURL: "https://go.dev/ref/spec#Declarations_and_scope",
	},

	"go_type_mismatch": {
		Pattern:     regexp.MustCompile(`cannot use .+ as .+ in`),
		Title:       "Type mismatch",
		Explanation: "A value of one type was used where a different type was expected. Go is strictly typed and does not perform implicit type conversions.",
		Suggestions: []string{
			"Use an explicit type conversion if the types are compatible",
			"Check that you are passing the correct variable",
			"Verify the function signature matches the arguments",
			"Consider using an interface if multiple types should be accepted",
		},
		Examples: []string{
			"int64(myInt) // explicit conversion",
			"strconv.Itoa(num) // int to string",
		},
		DocURL: "https://go.dev/ref/spec#Conversions",
	},

	"go_import_cycle": {
		Pattern:     regexp.MustCompile(`import cycle not allowed`),
		Title:       "Import cycle detected",
		Explanation: "Two or more packages import each other, creating a circular dependency. Go does not allow import cycles. This usually indicates a design issue where shared types or interfaces should be extracted to a separate package.",
		Suggestions: []string{
			"Extract shared types into a separate package",
			"Use interfaces to break the dependency",
			"Merge the packages if they are tightly coupled",
			"Use dependency injection to invert the dependency",
		},
		Examples: []string{
			"// Move shared types to a 'types' or 'common' package",
			"// Use an interface in package A instead of importing package B directly",
		},
		DocURL: "https://go.dev/doc/faq#mutual_import",
	},

	"go_too_many_args": {
		Pattern:     regexp.MustCompile(`too many arguments`),
		Title:       "Too many arguments in function call",
		Explanation: "More arguments were passed to a function than its signature accepts. This often happens after refactoring when a function signature changes.",
		Suggestions: []string{
			"Check the function signature for the expected number of parameters",
			"Remove extra arguments from the call",
			"Use variadic parameters if the function should accept variable arguments",
			"Check if you are calling the wrong function",
		},
		Examples: []string{
			"func foo(a, b int) {} // accepts exactly 2 args",
			"func bar(args ...int) {} // accepts variable args",
		},
	},

	"go_not_enough_args": {
		Pattern:     regexp.MustCompile(`not enough arguments`),
		Title:       "Not enough arguments in function call",
		Explanation: "Fewer arguments were passed to a function than its signature requires. Ensure all required parameters are provided.",
		Suggestions: []string{
			"Check the function signature for required parameters",
			"Add the missing arguments to the call",
			"Verify you are calling the correct function overload",
			"Check if a recent refactor added new parameters",
		},
		Examples: []string{
			"result := foo(a, b) // provide all required args",
		},
	},

	"go_deadlock": {
		Pattern:     regexp.MustCompile(`(fatal error: all goroutines are asleep|deadlock)`),
		Title:       "Goroutine deadlock",
		Explanation: "All goroutines are blocked waiting for each other, and no progress can be made. This typically happens when channels are used incorrectly or mutexes are locked in inconsistent order.",
		Suggestions: []string{
			"Ensure channels have a receiver before sending (or use buffered channels)",
			"Check for lock ordering issues with mutexes",
			"Use select with a default case or timeout to prevent blocking",
			"Verify that goroutines are actually started before waiting on them",
		},
		Examples: []string{
			"ch := make(chan int, 1) // buffered channel prevents blocking",
			"select {\ncase val := <-ch:\n    // handle\ncase <-time.After(5 * time.Second):\n    // timeout\n}",
		},
	},

	"go_nil_pointer": {
		Pattern:     regexp.MustCompile(`nil pointer dereference`),
		Title:       "Nil pointer dereference",
		Explanation: "A nil pointer was accessed. This means a variable was used before being initialized or after being set to nil.",
		Suggestions: []string{
			"Check if the variable is nil before accessing fields",
			"Look for functions that might return nil on error",
			"Add nil checks at the beginning of the function",
			"Initialize the variable before use",
		},
		Examples: []string{
			"if obj != nil {\n    obj.Method()\n}",
			"result, err := GetObj()\nif err != nil || result == nil {\n    return err\n}",
		},
	},

	"py_indentation": {
		Pattern:     regexp.MustCompile(`IndentationError`),
		Title:       "Python indentation error",
		Explanation: "Python uses indentation to define code blocks. Mixing tabs and spaces or inconsistent indentation levels will cause this error.",
		Suggestions: []string{
			"Use consistent indentation (4 spaces is recommended)",
			"Do not mix tabs and spaces",
			"Check that all blocks are aligned correctly",
			"Use an editor that shows whitespace characters",
		},
		Examples: []string{
			"def foo():\n    if True:\n        pass  # 4 spaces per level",
		},
	},

	"py_import": {
		Pattern:     regexp.MustCompile(`(ImportError|ModuleNotFoundError)`),
		Title:       "Python import error",
		Explanation: "The module or package could not be found. It may not be installed, or the module path may be incorrect.",
		Suggestions: []string{
			"Install the missing package with pip install <package>",
			"Check that the module name is spelled correctly",
			"Verify the virtual environment is activated",
			"Check that __init__.py exists for local packages",
			"Ensure the module is in PYTHONPATH",
		},
		Examples: []string{
			"pip install requests  # install missing package",
			"python -m venv venv && source venv/bin/activate",
		},
	},

	"py_type": {
		Pattern:     regexp.MustCompile(`TypeError`),
		Title:       "Python type error",
		Explanation: "An operation was applied to an object of inappropriate type. This often happens when mixing incompatible types or calling a non-callable object.",
		Suggestions: []string{
			"Check the types of all operands in the expression",
			"Use type() or isinstance() to verify types at runtime",
			"Convert types explicitly (str(), int(), float())",
			"Check that you are not accidentally overwriting a function name",
		},
		Examples: []string{
			"str(42) + \" items\"  # convert int to str before concatenation",
			"isinstance(obj, list)  # check type before use",
		},
	},

	"py_attribute": {
		Pattern:     regexp.MustCompile(`AttributeError`),
		Title:       "Python attribute error",
		Explanation: "An object does not have the attribute or method being accessed. This can happen when using the wrong type, a typo in the attribute name, or accessing an attribute before it is set.",
		Suggestions: []string{
			"Check the spelling of the attribute name",
			"Verify the object is of the expected type",
			"Use hasattr() to check if an attribute exists",
			"Check if the object is None when it should not be",
		},
		Examples: []string{
			"if hasattr(obj, 'method'):\n    obj.method()",
			"print(type(obj))  # verify the actual type",
		},
	},

	"js_module_not_found": {
		Pattern:     regexp.MustCompile(`Cannot find module`),
		Title:       "Module not found",
		Explanation: "The required module could not be resolved. It may not be installed, the path may be wrong, or type definitions may be missing.",
		Suggestions: []string{
			"Run npm install or yarn install to install dependencies",
			"Check that the module name is spelled correctly",
			"For relative imports, verify the file path exists",
			"Install type definitions with npm install @types/<package>",
			"Check tsconfig.json paths and baseUrl settings",
		},
		Examples: []string{
			"npm install lodash @types/lodash",
			"// Check relative path: import { foo } from './utils/foo'",
		},
	},

	"js_not_a_function": {
		Pattern:     regexp.MustCompile(`is not a function`),
		Title:       "Not a function",
		Explanation: "A value that is not a function was invoked as one. This usually means the variable holds undefined, null, or a non-function value at the time of the call.",
		Suggestions: []string{
			"Check that the import is correct and the function is exported",
			"Verify the object has the method you are calling",
			"Check for typos in the function name",
			"Ensure the value is not undefined or null before calling",
		},
		Examples: []string{
			"if (typeof fn === 'function') { fn(); }",
			"// Verify: export function myFunc() {} in the source module",
		},
	},

	"js_undefined_not_object": {
		Pattern:     regexp.MustCompile(`undefined is not an object`),
		Title:       "Cannot access property of undefined",
		Explanation: "An attempt was made to access a property on undefined. This means a previous property access or function call returned undefined.",
		Suggestions: []string{
			"Use optional chaining (?.) to safely access nested properties",
			"Add null/undefined checks before accessing properties",
			"Verify the data structure matches your expectations",
			"Check if an async operation completed before accessing results",
		},
		Examples: []string{
			"const name = obj?.user?.name ?? 'default';",
			"if (response && response.data) { /* use data */ }",
		},
	},

	"git_merge_conflict": {
		Pattern:     regexp.MustCompile(`(merge conflict|CONFLICT|Merge conflict)`),
		Title:       "Git merge conflict",
		Explanation: "Changes in different branches affect the same lines. Git cannot automatically determine which version to keep.",
		Suggestions: []string{
			"Open conflicted files and look for <<<<<<< markers",
			"Choose the correct version or combine both changes",
			"Remove the conflict markers after resolving",
			"Run git add on resolved files then git commit",
			"Consider using git mergetool for complex conflicts",
		},
		Examples: []string{
			"git status  # see which files have conflicts",
			"git add resolved_file.go && git commit",
		},
	},

	"git_not_a_repo": {
		Pattern:     regexp.MustCompile(`not a git repository`),
		Title:       "Not a git repository",
		Explanation: "The current directory (or specified path) is not inside a git repository. Either initialize a new repository or navigate to the correct directory.",
		Suggestions: []string{
			"Run git init to initialize a new repository",
			"Change to the correct project directory",
			"Check if a parent directory contains the .git folder",
			"Clone the repository if starting fresh",
		},
		Examples: []string{
			"git init",
			"cd /path/to/project && git status",
		},
	},

	"git_nothing_to_commit": {
		Pattern:     regexp.MustCompile(`nothing to commit`),
		Title:       "Nothing to commit",
		Explanation: "There are no staged or modified files to commit. All changes have already been committed or the working directory is clean.",
		Suggestions: []string{
			"Stage files with git add before committing",
			"Check git status to see if there are unstaged changes",
			"Verify you are in the correct branch",
			"Check if files are ignored by .gitignore",
		},
		Examples: []string{
			"git add . && git commit -m \"message\"",
			"git status  # check for untracked or modified files",
		},
	},

	"sys_permission_denied": {
		Pattern:     regexp.MustCompile(`permission denied`),
		Title:       "Permission denied",
		Explanation: "The operation was rejected due to insufficient filesystem or OS permissions. The current user does not have the required access rights.",
		Suggestions: []string{
			"Check file permissions with ls -la",
			"Use chmod to adjust permissions if appropriate",
			"Verify you are the file owner or use sudo if necessary",
			"Check if the file is read-only or locked by another process",
		},
		Examples: []string{
			"chmod 644 file.txt  # owner read/write, others read",
			"ls -la /path/to/file  # check current permissions",
		},
	},

	"sys_no_such_file": {
		Pattern:     regexp.MustCompile(`no such file or directory`),
		Title:       "File or directory not found",
		Explanation: "The specified path does not exist. The file may have been moved, deleted, or the path may contain a typo.",
		Suggestions: []string{
			"Verify the file path is correct",
			"Check for typos in the path",
			"Use ls or find to locate the file",
			"Create the directory with mkdir -p if needed",
			"Check if a relative path should be absolute",
		},
		Examples: []string{
			"mkdir -p /path/to/dir  # create missing directories",
			"find . -name 'filename'  # search for the file",
		},
	},

	"sys_address_in_use": {
		Pattern:     regexp.MustCompile(`address already in use`),
		Title:       "Address already in use",
		Explanation: "Another process is already listening on the requested port. Only one process can bind to a specific port at a time.",
		Suggestions: []string{
			"Find the process using the port with lsof or netstat",
			"Kill the existing process or choose a different port",
			"Wait for the previous process to release the port",
			"Set SO_REUSEADDR socket option if appropriate",
		},
		Examples: []string{
			"lsof -i :8080  # find process using port 8080",
			"kill -9 <PID>  # kill the process",
		},
	},

	"sys_connection_refused": {
		Pattern:     regexp.MustCompile(`connection refused`),
		Title:       "Connection refused",
		Explanation: "No service is listening on the target address and port. The server may not be running, or the address/port may be incorrect.",
		Suggestions: []string{
			"Verify the server is running",
			"Check the host and port are correct",
			"Look for firewall rules blocking the connection",
			"Check if the service is bound to localhost vs 0.0.0.0",
		},
		Examples: []string{
			"curl http://localhost:8080/health  # test connectivity",
			"netstat -tlnp | grep 8080  # check if port is listening",
		},
	},

	"sys_disk_full": {
		Pattern:     regexp.MustCompile(`(no space left on device|disk full)`),
		Title:       "Disk full",
		Explanation: "The filesystem has no remaining free space. Write operations will fail until space is freed.",
		Suggestions: []string{
			"Check disk usage with df -h",
			"Remove temporary files or old logs",
			"Empty trash and clear package caches",
			"Move large files to another volume",
		},
		Examples: []string{
			"df -h  # check free space",
			"du -sh /tmp/*  # find large temp files",
		},
	},

	"sys_timeout": {
		Pattern:     regexp.MustCompile(`(connection timed out|timeout|context deadline exceeded)`),
		Title:       "Operation timed out",
		Explanation: "The operation did not complete within the allowed time. This could be a network issue, an overloaded server, or an operation that needs a longer timeout.",
		Suggestions: []string{
			"Check network connectivity",
			"Increase the timeout value if the operation is expected to be slow",
			"Verify the remote service is responsive",
			"Check for DNS resolution issues",
		},
		Examples: []string{
			"ctx, cancel := context.WithTimeout(ctx, 30*time.Second)",
			"curl --connect-timeout 10 http://example.com",
		},
	},

	"rho_old_str_not_found": {
		Pattern:     regexp.MustCompile(`old_str not found`),
		Title:       "Edit target string not found",
		Explanation: "The text specified in old_str does not exist in the file. The file may have been modified since it was last read, or the string may contain whitespace or encoding differences.",
		Suggestions: []string{
			"Re-read the file to get the current content",
			"Check for leading/trailing whitespace differences",
			"Verify tabs vs spaces match exactly",
			"Ensure the string was not already modified by a previous edit",
			"Use a larger context string to ensure uniqueness",
		},
		Examples: []string{
			"// Re-read the file first, then retry the edit with exact content",
		},
		AutoFix: "Re-read the target file and retry with the exact current content",
	},

	"rho_file_too_large": {
		Pattern:     regexp.MustCompile(`file too large`),
		Title:       "File exceeds size limit",
		Explanation: "The file is too large to be processed in a single operation. This protects against accidentally loading very large files into memory.",
		Suggestions: []string{
			"Use head_tail to read specific line ranges",
			"Process the file in chunks",
			"Check if you are targeting the correct file",
			"Consider if the file can be split into smaller parts",
		},
		Examples: []string{
			"// Use offset and limit parameters to read a portion",
			"// head_tail tool: read first 50 and last 50 lines",
		},
		AutoFix: "Use head_tail or line-range reads to process the file in parts",
	},

	"rho_budget_exceeded": {
		Pattern:     regexp.MustCompile(`budget exceeded`),
		Title:       "Token or cost budget exceeded",
		Explanation: "The session has consumed more tokens or cost than the configured budget allows. This is a safety limit to prevent runaway costs.",
		Suggestions: []string{
			"Increase the budget limit if the task requires more tokens",
			"Use compaction to reduce context size",
			"Break the task into smaller sub-tasks",
			"Check for loops that may be inflating token usage",
		},
		Examples: []string{
			"rho --budget 10.00  # set a higher budget",
			"/compact  # reduce context size",
		},
	},

	"rho_tool_not_found": {
		Pattern:     regexp.MustCompile(`(tool not found|unknown tool)`),
		Title:       "Tool not found",
		Explanation: "The requested tool does not exist in the current tool registry. It may be misspelled or not available in this configuration.",
		Suggestions: []string{
			"Check the available tools list",
			"Verify the tool name spelling",
			"Check if the tool requires a plugin to be enabled",
			"Use a similar available tool instead",
		},
		Examples: []string{
			"// List available tools to find the correct name",
		},
	},

	"rho_path_boundary_violation": {
		Pattern:     regexp.MustCompile(`(path boundary violation|operation not permitted outside allowed path)`),
		Title:       "Path boundary violation",
		Explanation: "The operation was blocked because it attempted to access a resource outside the allowed project paths.",
		Suggestions: []string{
			"Check which paths are allowed by the project policy",
			"Request permission for the specific operation",
			"Verify the file is within the project directory",
			"Check Rho permission settings",
		},
		Examples: []string{
			"// Ensure operations target files within the project root",
		},
	},

	"go_unused_import": {
		Pattern:     regexp.MustCompile(`imported and not used`),
		Title:       "Unused import",
		Explanation: "A package was imported but none of its exported identifiers are used. Go treats unused imports as compile errors.",
		Suggestions: []string{
			"Remove the unused import",
			"Use the blank identifier _ if the import has side effects",
			"Add code that uses the package",
			"Use goimports to auto-manage imports",
		},
		Examples: []string{
			"import _ \"net/http/pprof\" // side-effect import",
			"// Run: goimports -w file.go",
		},
	},

	"go_unused_var": {
		Pattern:     regexp.MustCompile(`declared (and|but) not used`),
		Title:       "Unused variable",
		Explanation: "A variable was declared but never referenced. Go treats unused local variables as compile errors.",
		Suggestions: []string{
			"Remove the unused variable",
			"Use the blank identifier _ if the value should be discarded",
			"Add code that uses the variable",
		},
		Examples: []string{
			"_ = unusedValue // explicitly discard",
		},
	},

	"json_parse": {
		Pattern:     regexp.MustCompile(`(invalid character|unexpected end of JSON|json: cannot unmarshal)`),
		Title:       "JSON parse error",
		Explanation: "The input is not valid JSON or does not match the expected structure. This can happen with malformed data, trailing commas, or type mismatches.",
		Suggestions: []string{
			"Validate the JSON with a linter or jq",
			"Check for trailing commas (not allowed in JSON)",
			"Verify the JSON structure matches the target struct",
			"Check for unescaped special characters in strings",
		},
		Examples: []string{
			"echo '{\"key\": \"value\"}' | jq .  // validate JSON",
			"json.Unmarshal(data, &target)  // ensure target matches structure",
		},
	},

	"go_interface_not_implemented": {
		Pattern:     regexp.MustCompile(`does not implement`),
		Title:       "Interface not satisfied",
		Explanation: "A type was used where an interface is expected, but it does not implement all required methods. Check the missing method signatures.",
		Suggestions: []string{
			"Add the missing method(s) to the type",
			"Check method signatures match exactly (receiver, params, return types)",
			"Use a pointer receiver if the interface requires it",
			"Verify you are implementing the correct interface",
		},
		Examples: []string{
			"// Compile-time check:\nvar _ MyInterface = (*MyType)(nil)",
		},
	},

	"oom": {
		Pattern:     regexp.MustCompile(`(out of memory|OOM|cannot allocate memory)`),
		Title:       "Out of memory",
		Explanation: "The process attempted to allocate more memory than is available. This can happen with large data sets, memory leaks, or insufficient system resources.",
		Suggestions: []string{
			"Process data in smaller batches",
			"Check for memory leaks or unbounded growth",
			"Increase available memory or swap space",
			"Profile memory usage to find the allocation hotspot",
		},
		Examples: []string{
			"// Process in batches instead of loading all at once",
			"GOGC=50 ./myapp  // more aggressive garbage collection",
		},
	},
}

func (ec *ErrorContext) Enrich(err string) *EnrichedError {
	ec.mu.RLock()
	defer ec.mu.RUnlock()

	errLower := strings.ToLower(err)

	var bestHelp *ErrorHelp
	bestLen := 0

	for _, help := range ec.Patterns {
		if help.Pattern.MatchString(err) || help.Pattern.MatchString(errLower) {
			patLen := len(help.Pattern.String())
			if bestHelp == nil || patLen > bestLen {
				bestHelp = help
				bestLen = patLen
			}
		}
	}

	if bestHelp == nil {
		return nil
	}

	return &EnrichedError{
		Original:    err,
		Title:       bestHelp.Title,
		Explanation: bestHelp.Explanation,
		Suggestions: bestHelp.Suggestions,
		Examples:    bestHelp.Examples,
		Severity:    classifySeverity(err),
		Recoverable: classifyRecoverable(err),
	}
}

func FormatError(enriched *EnrichedError) string {
	if enriched == nil {
		return ""
	}

	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Error: %s\n", enriched.Title))
	sb.WriteString("─────────────────────────────────\n")
	sb.WriteString(enriched.Explanation)
	sb.WriteString("\n")

	if len(enriched.Suggestions) > 0 {
		sb.WriteString("\nSuggestions:\n")
		for _, s := range enriched.Suggestions {
			sb.WriteString(fmt.Sprintf("• %s\n", s))
		}
	}

	if len(enriched.Examples) > 0 {
		sb.WriteString("\nExample fix:\n")
		for _, e := range enriched.Examples {
			sb.WriteString(fmt.Sprintf("  %s\n", e))
		}
	}

	recoverable := "no"
	if enriched.Recoverable {
		recoverable = "yes"
	}
	sb.WriteString(fmt.Sprintf("\nSeverity: %s | Recoverable: %s\n", enriched.Severity, recoverable))

	return sb.String()
}

func (ec *ErrorContext) AddPattern(pattern string, help ErrorHelp) error {
	compiled, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Errorf("invalid pattern %q: %w", pattern, err)
	}

	help.Pattern = compiled

	ec.mu.Lock()
	defer ec.mu.Unlock()

	key := sanitizePatternKey(pattern)
	ec.Patterns[key] = &help

	return nil
}

func (ec *ErrorContext) IsRecoverable(err string) bool {
	enriched := ec.Enrich(err)
	if enriched != nil {
		return enriched.Recoverable
	}
	return classifyRecoverable(err)
}

func (ec *ErrorContext) SuggestFix(err string) string {
	ec.mu.RLock()
	defer ec.mu.RUnlock()

	errLower := strings.ToLower(err)

	var bestHelp *ErrorHelp
	bestLen := 0

	for _, help := range ec.Patterns {
		if help.Pattern.MatchString(err) || help.Pattern.MatchString(errLower) {
			patLen := len(help.Pattern.String())
			if bestHelp == nil || patLen > bestLen {
				bestHelp = help
				bestLen = patLen
			}
		}
	}

	if bestHelp == nil {
		return ""
	}

	if bestHelp.AutoFix != "" {
		return bestHelp.AutoFix
	}
	if len(bestHelp.Suggestions) > 0 {
		return bestHelp.Suggestions[0]
	}
	return ""
}

func classifySeverity(err string) string {
	errLower := strings.ToLower(err)

	critical := []string{"panic", "fatal", "deadlock", "nil pointer", "out of memory", "oom", "segfault"}
	for _, kw := range critical {
		if strings.Contains(errLower, kw) {
			return "CRITICAL"
		}
	}

	high := []string{"permission denied", "import cycle", "merge conflict", "budget exceeded", "path boundary violation"}
	for _, kw := range high {
		if strings.Contains(errLower, kw) {
			return "HIGH"
		}
	}

	low := []string{"nothing to commit", "unused", "not used"}
	for _, kw := range low {
		if strings.Contains(errLower, kw) {
			return "LOW"
		}
	}

	return "MEDIUM"
}

func classifyRecoverable(err string) bool {
	errLower := strings.ToLower(err)

	unrecoverable := []string{
		"panic", "fatal", "deadlock", "nil pointer dereference",
		"nil pointer", "out of memory", "oom", "segfault",
		"disk full", "no space left on device",
	}
	for _, kw := range unrecoverable {
		if strings.Contains(errLower, kw) {
			return false
		}
	}

	return true
}

func sanitizePatternKey(pattern string) string {
	key := strings.ToLower(pattern)
	key = strings.ReplaceAll(key, " ", "_")
	key = strings.ReplaceAll(key, "\\", "")
	key = strings.ReplaceAll(key, "(", "")
	key = strings.ReplaceAll(key, ")", "")
	key = strings.ReplaceAll(key, ".", "")
	key = strings.ReplaceAll(key, "+", "")
	key = strings.ReplaceAll(key, "*", "")
	key = strings.ReplaceAll(key, "?", "")
	if len(key) > 50 {
		key = key[:50]
	}
	return "custom_" + key
}
