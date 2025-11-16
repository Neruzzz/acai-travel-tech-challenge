package tools

import "context"

// Contract common to all tools.
type Tool interface {
	Name() string
	Description() string
	ParametersSchema() map[string]any
	Call(ctx context.Context, args map[string]any) (string, error)
}

var registry []Tool

// Register adds a tool to the registry.
func Register(t Tool) {
	registry = append(registry, t)
}

// AllTools returns all registered tools.
func AllTools() []Tool {
	return registry
}

// FindByName searches a tool by its name in the registry.
func FindByName(name string) Tool {
	for _, t := range registry {
		if t.Name() == name {
			return t
		}
	}
	return nil
}
