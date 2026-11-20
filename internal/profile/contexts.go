package profile

import (
	"errors"
	"os"
	"regexp"
	"sort"

	"github.com/ibm-verify/agent-identity-cli/internal/i18n"
)

const (
	// DefaultContext is the context used when nothing else selects one.
	DefaultContext = "default"

	// EnvContext selects the context when no flag is given.
	EnvContext = "IBM_AGENTID_CONTEXT"
)

// Sentinel errors for matching with errors.Is. The errors returned by Config
// methods wrap these with localized messages.
var (
	ErrContextNotFound    = errors.New("context not found")
	ErrContextExists      = errors.New("context already exists")
	ErrInvalidContextName = errors.New("invalid context name")
)

// Context names double as cache file names, so they are restricted to a safe set.
var contextNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// ValidateName reports whether name is usable as a context name. It fails with
// ErrInvalidContextName otherwise.
func ValidateName(name string) error {
	if !contextNamePattern.MatchString(name) {
		return i18n.Wrap(ErrInvalidContextName, "context.invalid_name", map[string]any{"Name": name})
	}
	return nil
}

func notFound(name string) error {
	return i18n.Wrap(ErrContextNotFound, "context.not_found", map[string]any{"Name": name})
}

// Names returns the registered context names in sorted order.
func (c *Config) Names() []string {
	names := make([]string, 0, len(c.Contexts))
	for name := range c.Contexts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Get returns the named context.
func (c *Config) Get(name string) (*Environment, error) {
	env, ok := c.Contexts[name]
	if !ok {
		return nil, notFound(name)
	}
	return env, nil
}

// Set creates the named context or replaces it if it exists, then saves
// config.yaml.
func (c *Config) Set(name string, env *Environment) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	if env == nil {
		return i18n.Errorf("context.nil_environment", map[string]any{"Name": name})
	}
	if c.Contexts == nil {
		c.Contexts = map[string]*Environment{}
	}
	c.Contexts[name] = env
	return c.Save()
}

// Use makes the named context the current one, then saves config.yaml.
func (c *Config) Use(name string) error {
	if _, ok := c.Contexts[name]; !ok {
		return notFound(name)
	}
	c.CurrentContext = name
	return c.Save()
}

// Delete removes the named context and saves config.yaml. If it was the
// current context, the current context is cleared. The caller is responsible
// for removing the context's cache file.
func (c *Config) Delete(name string) error {
	if _, ok := c.Contexts[name]; !ok {
		return notFound(name)
	}
	delete(c.Contexts, name)
	if c.CurrentContext == name {
		c.CurrentContext = ""
	}
	return c.Save()
}

// Rename changes a context's name, moves the current-context pointer with it,
// and saves config.yaml. The caller is responsible for moving the context's
// cache file.
func (c *Config) Rename(oldName, newName string) error {
	env, ok := c.Contexts[oldName]
	if !ok {
		return notFound(oldName)
	}
	if err := ValidateName(newName); err != nil {
		return err
	}
	if oldName == newName {
		return nil
	}
	if _, exists := c.Contexts[newName]; exists {
		return i18n.Wrap(ErrContextExists, "context.exists", map[string]any{"Name": newName})
	}
	c.Contexts[newName] = env
	delete(c.Contexts, oldName)
	if c.CurrentContext == oldName {
		c.CurrentContext = newName
	}
	return c.Save()
}

// ResolveName picks the context name to use. Precedence: flagValue, the
// IBM_AGENTID_CONTEXT environment variable, current-context, then "default".
func (c *Config) ResolveName(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if v := os.Getenv(EnvContext); v != "" {
		return v
	}
	if c.CurrentContext != "" {
		return c.CurrentContext
	}
	return DefaultContext
}

// Resolve returns the name and environment of the selected context. It fails
// with ErrContextNotFound if the selected context is not registered.
func (c *Config) Resolve(flagValue string) (string, *Environment, error) {
	name := c.ResolveName(flagValue)
	env, err := c.Get(name)
	if err != nil {
		return name, nil, err
	}
	return name, env, nil
}
