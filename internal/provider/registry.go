package provider

import (
	"fmt"
	"strings"
	"sync"
)

// factories is the global registry of provider factories keyed by name.
var (
	mu        sync.RWMutex
	factories = map[string]Factory{}
)

// Factory builds a Provider for a given config token. Tokens are typically
// fetched from env or the .reproforge config.
type Factory func(token string) Provider

// Register adds a factory to the global registry. Registering twice with the
// same name overrides the previous factory; this is by design so tests can
// substitute providers cleanly.
func Register(name string, f Factory) {
	mu.Lock()
	defer mu.Unlock()
	factories[strings.ToLower(name)] = f
}

// Get returns the factory for name, or an error.
func Get(name string) (Factory, error) {
	mu.RLock()
	defer mu.RUnlock()
	f, ok := factories[strings.ToLower(name)]
	if !ok {
		return nil, fmt.Errorf("provider: %q is not registered", name)
	}
	return f, nil
}

// Names returns the registered provider names sorted lexicographically.
func Names() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(factories))
	for k := range factories {
		out = append(out, k)
	}
	return out
}

// Detect returns the provider whose Parse accepts the given reference. The
// detection order is: github_actions, gitlab_ci, buildkite. If no provider
// claims the reference an error is returned.
func Detect(reference string) (string, error) {
	candidates := []string{"github_actions", "gitlab_ci", "buildkite"}
	for _, n := range candidates {
		f, err := Get(n)
		if err != nil {
			continue
		}
		p := f("")
		if _, err := p.Parse(reference); err == nil {
			return n, nil
		}
	}
	return "", fmt.Errorf("provider: cannot detect provider for reference %q", reference)
}
