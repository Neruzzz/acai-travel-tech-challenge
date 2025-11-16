package tools

import "context"

// Tool define el contrato mínimo que expone cada herramienta.
type Tool interface {
	Name() string                                                  // nombre exacto que verá el modelo (p. ej., "get_weather")
	Description() string                                           // descripción breve para el modelo
	ParametersSchema() map[string]any                              // JSON schema (object) de parámetros
	Call(ctx context.Context, args map[string]any) (string, error) // ejecución
}

var registry []Tool

// Register lo llamas en init() de cada tool.
func Register(t Tool) {
	registry = append(registry, t)
}

// AllTools devuelve todas las tools registradas.
func AllTools() []Tool {
	return registry
}

// FindByName busca una tool ya registrada por su nombre.
func FindByName(name string) Tool {
	for _, t := range registry {
		if t.Name() == name {
			return t
		}
	}
	return nil
}
