package cmd

import (
	"fmt"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
	commandfeature "github.com/GrayCodeAI/rho/internal/features/commands"
)

// ChatSubcommand is a single slash-command handler. Implementations live in
// their own cmd/chat_subcommand_<name>.go file and register themselves with
// SubcommandRegistry. Keeping the interface here makes the dispatcher a
// small composition boundary instead of another command implementation file.
type ChatSubcommand interface {
	// Name is the canonical command name WITHOUT the leading slash.
	// e.g. "help" for "/help". Names are lowercase.
	Name() string

	// Aliases are alternative names that dispatch to the same
	// implementation. e.g. "exit" and "quit" both map to the
	// session command. Empty for no aliases.
	Aliases() []string

	// Description is a one-line help string shown in /help output.
	// Keep under 60 characters to fit the help column.
	Description() string

	// Usage is shown when the user provides invalid arguments.
	// Empty for argument-free commands.
	Usage() string

	// Handle dispatches the command. The chat model is the
	// receiver for state access. args is the parsed argument list
	// (without the command name); text is the original raw text
	// (including the command name) for cases that need to
	// re-parse (e.g., quoted strings).
	//
	// The returned tea.Model is the (possibly new) model; tea.Cmd
	// is an optional side-effect to enqueue (tea.Quit for /quit).
	Handle(m *chatModel, args []string, text string) (tea.Model, tea.Cmd)
}

// SubcommandRegistry is the canonical index mapping slash command
// names (and their aliases) to ChatSubcommand implementations. It's
// safe for concurrent use.
type SubcommandRegistry struct {
	mu                 sync.RWMutex
	metadata           *commandfeature.Registry
	registrationErrors []string
	// primary remains the typed handler index at the composition boundary.
	// Metadata and alias policy live in the feature registry.
	primary map[string]ChatSubcommand
}

// subcommandRegistry is the package-level registry that subcommand
// implementations register with via init functions. handleCommand resolves
// commands through this registry, then handles plugin and unknown-command
// fallbacks at the composition boundary.
//
// Subcommand files should NOT construct their own registry; they
// should call subcommandRegistry.Register(&mySubcommand{}) in an
// init() function.
var subcommandRegistry = NewSubcommandRegistry()

// validateChatCommandComposition checks the executable command surface after
// all package init functions have registered their handlers. Keeping this at
// the composition boundary prevents help/completion metadata from silently
// drifting away from actual dispatch.
func validateChatCommandComposition() error {
	if err := commandfeature.ValidateBuiltIns(); err != nil {
		return err
	}
	if errs := subcommandRegistry.RegistrationErrors(); len(errs) > 0 {
		return fmt.Errorf("slash command registration failed: %s", strings.Join(errs, "; "))
	}
	for _, name := range commandfeature.BuiltInNames() {
		if _, ok := subcommandRegistry.Lookup(strings.TrimPrefix(name, "/")); !ok {
			return fmt.Errorf("built-in command %s has no registered handler", name)
		}
	}
	return nil
}

// NewSubcommandRegistry creates an empty registry. Subcommands are
// registered via Register() (typically from per-file init() funcs
// or from a single aggregate init that imports each subcommand).
func NewSubcommandRegistry() *SubcommandRegistry {
	return &SubcommandRegistry{
		metadata: commandfeature.NewRegistry(),
		primary:  make(map[string]ChatSubcommand),
	}
}

// Register adds a subcommand to the registry. The primary name and
// all aliases are indexed. If a name is already registered, this
// is a no-op (the existing entry is kept) — duplicate registration
// is treated as a configuration error but doesn't panic, so test
// ordering and re-init don't blow up the binary. The same applies
// to alias collisions: if any of the subcommand's aliases is already
// registered (either as a primary or as another alias), registration
// is rejected (see M5 in the code review).
func (r *SubcommandRegistry) Register(cmd ChatSubcommand) {
	if cmd == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	name := cmd.Name()
	if !r.metadata.Register(commandfeature.Spec{
		Name: name, Aliases: cmd.Aliases(), Description: cmd.Description(), Usage: cmd.Usage(),
	}) {
		r.registrationErrors = append(r.registrationErrors, fmt.Sprintf("%q (duplicate, malformed, or colliding alias)", name))
		return
	}
	r.primary[name] = cmd
}

// RegistrationErrors reports rejected handlers without exposing mutable
// registry internals. Composition validation can therefore fail fast with an
// actionable diagnostic instead of silently shipping a missing command.
func (r *SubcommandRegistry) RegistrationErrors() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]string(nil), r.registrationErrors...)
}

// Lookup returns the subcommand for a slash name (without the
// leading slash). The second return is false if neither the name
// nor any of its aliases is registered.
func (r *SubcommandRegistry) Lookup(name string) (ChatSubcommand, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	spec, ok := r.metadata.Lookup(name)
	if !ok {
		return nil, false
	}
	cmd, ok := r.primary[spec.Name]
	return cmd, ok
}

// Names returns all primary command names in sorted order. Used by
// /help and /commands to enumerate available subcommands.
func (r *SubcommandRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	metadata := r.metadata.Names()
	names := make([]string, 0, len(metadata))
	for _, name := range metadata {
		if _, ok := r.primary[name]; ok {
			names = append(names, name)
		}
	}
	return names
}

// All returns all registered subcommands (deduplicated by primary
// name). Used by /help to render the full help table.
func (r *SubcommandRegistry) All() []ChatSubcommand {
	r.mu.RLock()
	defer r.mu.RUnlock()
	metadata := r.metadata.All()
	out := make([]ChatSubcommand, 0, len(metadata))
	for _, spec := range metadata {
		if cmd, ok := r.primary[spec.Name]; ok {
			out = append(out, cmd)
		}
	}
	return out
}

// Size returns the number of primary subcommands (excluding
// aliases). Used by tests and by /commands to show a count.
func (r *SubcommandRegistry) Size() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.primary)
}
