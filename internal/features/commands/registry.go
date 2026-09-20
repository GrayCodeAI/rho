package commands

import (
	"sort"
	"strings"
	"sync"
)

// Spec is command metadata independent of any UI or handler implementation.
type Spec struct {
	Name        string
	Aliases     []string
	Description string
	Usage       string
}

// Registry owns canonical names, aliases, collision policy, and deterministic
// enumeration. Frontends attach their own typed handlers to these specs.
type Registry struct {
	mu      sync.RWMutex
	primary map[string]Spec
	aliasOf map[string]string
}

func NewRegistry() *Registry {
	return &Registry{primary: make(map[string]Spec), aliasOf: make(map[string]string)}
}

func (r *Registry) Register(spec Spec) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !validName(spec.Name) {
		return false
	}
	if _, exists := r.primary[spec.Name]; exists {
		return false
	}
	if _, exists := r.aliasOf[spec.Name]; exists {
		return false
	}
	aliasesSeen := make(map[string]struct{}, len(spec.Aliases))
	for _, alias := range spec.Aliases {
		if !validName(alias) || alias == spec.Name {
			return false
		}
		if _, duplicate := aliasesSeen[alias]; duplicate {
			return false
		}
		aliasesSeen[alias] = struct{}{}
		if _, exists := r.primary[alias]; exists {
			return false
		}
		if _, exists := r.aliasOf[alias]; exists {
			return false
		}
	}
	r.primary[spec.Name] = Spec{
		Name: spec.Name, Aliases: append([]string(nil), spec.Aliases...),
		Description: spec.Description, Usage: spec.Usage,
	}
	for _, alias := range spec.Aliases {
		if alias != "" {
			r.aliasOf[alias] = spec.Name
		}
	}
	return true
}

func validName(name string) bool {
	return name != "" && strings.TrimSpace(name) == name &&
		!strings.HasPrefix(name, "/") && !strings.ContainsAny(name, " \t\r\n")
}

func (r *Registry) Lookup(name string) (Spec, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if spec, ok := r.primary[name]; ok {
		return cloneSpec(spec), true
	}
	primary, ok := r.aliasOf[name]
	if !ok {
		return Spec{}, false
	}
	spec, ok := r.primary[primary]
	return cloneSpec(spec), ok
}

func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.primary))
	for name := range r.primary {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (r *Registry) All() []Spec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	specs := make([]Spec, 0, len(r.primary))
	for _, spec := range r.primary {
		specs = append(specs, cloneSpec(spec))
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].Name < specs[j].Name })
	return specs
}

func (r *Registry) Size() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.primary)
}

func cloneSpec(spec Spec) Spec {
	spec.Aliases = append([]string(nil), spec.Aliases...)
	return spec
}
