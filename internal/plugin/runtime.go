package plugin

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/GrayCodeAI/rho/internal/hooks"
)

// Runtime manages loaded plugins and their execution.
type Runtime struct {
	plugins     []*Manifest
	commands    map[string]CommandDef
	hooks       map[string][]HookDef
	SmartSkills []SmartSkill
}

// NewRuntime creates a new plugin runtime.
func NewRuntime() *Runtime {
	return &Runtime{
		commands: make(map[string]CommandDef),
		hooks:    make(map[string][]HookDef),
	}
}

// LoadAll loads all installed plugins.
func (r *Runtime) LoadAll() error {
	plugins, err := List()
	if err != nil {
		return err
	}
	if err := r.rebuildIndexes(plugins); err != nil {
		return err
	}
	r.plugins = plugins
	// Load smart skills from standard directories
	r.SmartSkills = LoadSmartSkills(DefaultSkillDirs())
	return nil
}

func (r *Runtime) rebuildIndexes(plugins []*Manifest) error {
	// Rebuild derived indexes from the authoritative manifest set. Without
	// clearing these maps, a reload leaves commands and hooks from removed
	// plugins executable for the rest of the process lifetime.
	commands := make(map[string]CommandDef)
	hooks := make(map[string][]HookDef)
	for _, p := range plugins {
		for _, cmd := range p.Commands {
			if err := validateRuntimeCommandName(cmd.Name); err != nil {
				return fmt.Errorf("plugin %q: %w", p.Name, err)
			}
			if owner, exists := commands[cmd.Name]; exists {
				return fmt.Errorf("plugin command %q is declared more than once (already registered as %q)", cmd.Name, owner.Name)
			}
			commands[cmd.Name] = cmd
		}
		for _, h := range p.Hooks {
			hooks[h.Event] = append(hooks[h.Event], h)
		}
	}
	r.commands = commands
	r.hooks = hooks
	return nil
}

func validateRuntimeCommandName(name string) error {
	if name == "" {
		return fmt.Errorf("command name is required")
	}
	if strings.TrimSpace(name) != name || strings.HasPrefix(name, "/") || strings.ContainsAny(name, " \t\r\n") {
		return fmt.Errorf("invalid command name %q", name)
	}
	return nil
}

// ExecuteCommand runs a plugin command.
func (r *Runtime) ExecuteCommand(name string, args []string) (string, error) {
	cmd, ok := r.commands[name]
	if !ok {
		return "", fmt.Errorf("unknown plugin command: %s", name)
	}
	if cmd.Script == "" {
		return "", fmt.Errorf("command %s has no script", name)
	}
	ctx := context.Background()
	c := exec.CommandContext(ctx, "bash", "-c", cmd.Script) // #nosec G204 -- cmd.Script comes from a locally installed plugin's own manifest, trusted like other plugin config
	c.Args = append(c.Args, args...)
	c.Dir = filepath.Join(pluginsDir(), cmd.Name)
	out, err := c.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("command failed: %w", err)
	}
	return string(out), nil
}

// RegisterHooks registers all plugin hooks with the hook registry.
func pluginHookEnvKey(key string) string {
	var b strings.Builder
	b.WriteString("RHO_")
	for _, r := range strings.ToUpper(key) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == len("RHO_") {
		b.WriteString("DATA")
	}
	return b.String()
}

func (r *Runtime) RegisterHooks() {
	for event, hookList := range r.hooks {
		for _, h := range hookList {
			cmd := h.Command
			hooks.Register(hooks.Hook{
				Name:  fmt.Sprintf("plugin:%s", event),
				Event: hooks.EventType(event),
				Fn: func(ctx context.Context, data map[string]interface{}) error {
					c := exec.CommandContext(ctx, "bash", "-c", cmd) // #nosec G204 -- cmd comes from a locally installed plugin's own manifest, trusted like other plugin config
					c.Env = os.Environ()
					for k, v := range data {
						c.Env = append(c.Env, fmt.Sprintf("%s=%v", pluginHookEnvKey(k), v))
					}
					out, err := c.CombinedOutput()
					if err != nil {
						return fmt.Errorf("hook failed: %w\n%s", err, string(out))
					}
					return nil
				},
			})
		}
	}
}

// CommandList returns all available plugin commands.
func (r *Runtime) CommandList() []CommandDef {
	var out []CommandDef
	for _, cmd := range r.commands {
		out = append(out, cmd)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// IsCommand checks if a name is a plugin command.
func (r *Runtime) IsCommand(name string) bool {
	_, ok := r.commands[name]
	return ok
}

// ListPlugins returns all loaded plugin manifests.
func (r *Runtime) ListPlugins() []*Manifest {
	return r.plugins
}
