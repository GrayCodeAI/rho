package shellmode

import (
	"os/exec"
	"regexp"
	"strings"
	"sync"
)

// Classification is the result of input analysis.
type Classification int

const (
	ClassShell   Classification = iota // execute in shell
	ClassAgent                         // route to AI agent
	ClassNeutral                       // undetermined
)

// agentWords are conversational words that always route to agent.
var agentWords = map[string]bool{
	// affirmations
	"yes": true, "yeah": true, "yep": true, "sure": true, "ok": true, "okay": true,
	"absolutely": true, "definitely": true, "certainly": true, "correct": true, "exactly": true,
	"perfect": true, "agreed": true, "lgtm": true,
	// negations
	"no": true, "nope": true, "nah": true, "never": true, "wrong": true,
	// gratitude
	"thanks": true, "thank": true, "thx": true, "ty": true, "cheers": true,
	// reactions
	"great": true, "good": true, "nice": true, "cool": true, "awesome": true,
	"amazing": true, "wonderful": true, "brilliant": true, "excellent": true,
	// greetings
	"hey": true, "hi": true, "hello": true, "bye": true,
	// conversational
	"please": true, "sorry": true, "hmm": true, "well": true,
	// action/intent
	"explain": true, "elaborate": true, "clarify": true, "summarize": true,
	"describe": true, "show": true, "tell": true,
	// question words
	"why": true, "how": true, "what": true, "when": true, "where": true, "who": true, "which": true,
	// programming verbs (not real commands)
	"refactor": true, "optimize": true, "scaffold": true,
}

// shellReservedWords pass `command -v` but are never standalone commands.
var shellReservedWords = map[string]bool{
	"do": true, "done": true, "then": true, "else": true, "elif": true,
	"fi": true, "esac": true, "in": true, "select": true, "function": true,
}

// unambiguousCommands are dev-tool/builtin names that are essentially never
// used as an English verb at the start of a sentence directed at an AI
// ("git commit", "npm install", "docker ps" — nobody phrases a request to
// an agent that way). When one of these is the first word, the rest of the
// input is trusted as shell arguments without further scrutiny.
var unambiguousCommands = map[string]bool{
	"git": true, "npm": true, "yarn": true, "pnpm": true, "docker": true, "docker-compose": true,
	"kubectl": true, "curl": true, "wget": true, "ssh": true, "scp": true, "rsync": true,
	"python": true, "python3": true, "node": true, "cargo": true, "rustc": true,
	"brew": true, "apt": true, "apt-get": true, "systemctl": true, "journalctl": true,
	"ls": true, "cd": true, "pwd": true, "mkdir": true, "rmdir": true, "rm": true, "cp": true, "mv": true,
	"chmod": true, "chown": true, "grep": true, "sed": true, "awk": true,
	"tar": true, "gzip": true, "gunzip": true, "zip": true, "unzip": true,
	"ps": true, "df": true, "du": true, "ping": true, "dig": true, "nslookup": true,
	"vim": true, "nvim": true, "nano": true, "code": true, "gh": true,
	"psql": true, "mysql": true, "redis-cli": true, "ffmpeg": true, "jq": true, "htop": true,
	"echo": true, "printf": true, "pip": true, "pip3": true, "gem": true, "bundle": true,
	"terraform": true, "ansible": true, "helm": true, "aws": true, "gcloud": true, "az": true,
	"java": true, "javac": true, "mvn": true, "gradle": true, "ruby": true, "php": true, "perl": true,
	"cat": true,
}

// ClassifyInput determines whether input should go to shell or AI agent.
// This is the single source of truth for routing decisions in auto mode.
func ClassifyInput(input string) Classification {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return ClassNeutral
	}

	words := strings.Fields(trimmed)
	firstWord := strings.ToLower(words[0])

	// Layer 0: Agent words — always route to agent
	if agentWords[firstWord] {
		return ClassAgent
	}

	// Layer 1: Shell reserved words — route to agent
	if shellReservedWords[firstWord] {
		return ClassAgent
	}

	if !isValidCommand(firstWord) {
		// Not a real command at all — plain natural language.
		return ClassAgent
	}

	if len(words) == 1 {
		// A single bare word that resolves to a real binary is unambiguous
		// either way — trust it as shell (e.g. "ls", "pwd").
		return ClassShell
	}

	if unambiguousCommands[firstWord] {
		return ClassShell
	}

	// The first word happens to resolve to a real PATH binary, but it's not
	// on the "definitely a dev tool" list — many ordinary English verbs
	// double as Unix commands (make, test, find, kill, time, sort, diff,
	// patch, more, less, man, who, date, file, which, look...). Naively
	// trusting isValidCommand() here misclassifies sentences like "make
	// sure this works" or "find the bug in this file" as shell commands.
	// Warp's own autodetect hits the same ambiguity and resolves it with a
	// user-configurable "natural language denylist" for exactly these
	// words — same idea here, except we require the input to actually look
	// like shell syntax (flags, paths, pipes/redirects) before trusting it,
	// rather than hand-maintaining an exhaustive list of ambiguous words.
	if hasShellSyntaxEvidence(trimmed) {
		return ClassShell
	}
	return ClassAgent
}

// fileExtPattern matches a trailing dotted file extension (.go, .txt,
// .json, .py...). Plain English sentences essentially never produce a
// word shaped like this — decimals ("3.5") are excluded by requiring the
// extension to start with a letter, and short trailing abbreviations
// ("e.g.", "Mr.") are excluded by requiring at least 2 characters after
// the dot.
var fileExtPattern = regexp.MustCompile(`\.[A-Za-z][A-Za-z0-9]{1,4}$`)

// hasShellSyntaxEvidence reports whether s contains a signal that only
// appears in real shell usage, not in a plain English sentence: a flag
// (-x/--flag), a path separator or bare filename with an extension, or a
// shell operator (pipe, redirect, sequencing, variable expansion, glob).
func hasShellSyntaxEvidence(s string) bool {
	if strings.ContainsAny(s, "|<>;&$`*") {
		return true
	}
	for _, w := range strings.Fields(s) {
		if len(w) >= 2 && w[0] == '-' {
			return true
		}
		if strings.Contains(w, "/") {
			return true
		}
		if fileExtPattern.MatchString(w) {
			return true
		}
	}
	return false
}

// cmdCache caches exec.LookPath results to avoid repeated PATH scans.
var (
	cmdCacheMu sync.RWMutex
	cmdCache   = make(map[string]bool)
)

// isValidCommand checks if a word exists as a command in PATH (cached).
func isValidCommand(word string) bool {
	cmdCacheMu.RLock()
	if v, ok := cmdCache[word]; ok {
		cmdCacheMu.RUnlock()
		return v
	}
	cmdCacheMu.RUnlock()
	_, err := exec.LookPath(word)
	result := err == nil
	cmdCacheMu.Lock()
	cmdCache[word] = result
	cmdCacheMu.Unlock()
	return result
}
