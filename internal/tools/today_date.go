package tools

import (
	"context"
	"time"
)

type ToolTodayDate struct{}

func (ToolTodayDate) Name() string { return "get_today_date" }

func (ToolTodayDate) Description() string {
	return "Get today's date and time in RFC3339 format."
}

func (ToolTodayDate) ParametersSchema() map[string]any {
	// no parameters
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

func (ToolTodayDate) Call(ctx context.Context, _ map[string]any) (string, error) {
	return time.Now().Format(time.RFC3339), nil
}

func init() {
	Register(ToolTodayDate{})
}
