package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	ics "github.com/arran4/golang-ical"
)

type ToolHolidays struct{}

func (ToolHolidays) Name() string { return "get_holidays" }

func (ToolHolidays) Description() string {
	return "Gets local bank and public holidays. Each line is 'YYYY-MM-DD: Holiday Name'."
}

func (ToolHolidays) ParametersSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"before_date": map[string]any{"type": "string", "description": "Optional RFC3339 date. Return holidays before this date."},
			"after_date":  map[string]any{"type": "string", "description": "Optional RFC3339 date. Return holidays after this date."},
			"max_count":   map[string]any{"type": "integer", "description": "Optional maximum number of holidays."},
		},
	}
}

func (ToolHolidays) Call(ctx context.Context, args map[string]any) (string, error) {
	link := "https://www.officeholidays.com/ics/spain/catalonia"
	if v := os.Getenv("HOLIDAY_CALENDAR_LINK"); strings.TrimSpace(v) != "" {
		link = v
	}

	events, err := loadCalendar(ctx, link)
	if err != nil {
		return "", err
	}

	var before, after time.Time
	var maxCount int

	if s, _ := args["before_date"].(string); s != "" {
		if t, e := time.Parse(time.RFC3339, s); e == nil {
			before = t
		}
	}
	if s, _ := args["after_date"].(string); s != "" {
		if t, e := time.Parse(time.RFC3339, s); e == nil {
			after = t
		}
	}
	if n, ok := args["max_count"].(float64); ok {
		maxCount = int(n)
	}

	var out []string
	for _, ev := range events {
		d, e := ev.GetAllDayStartAt()
		if e != nil {
			continue
		}
		if !before.IsZero() && d.After(before) {
			continue
		}
		if !after.IsZero() && d.Before(after) {
			continue
		}
		out = append(out, d.Format(time.DateOnly)+": "+ev.GetProperty(ics.ComponentPropertySummary).Value)
		if maxCount > 0 && len(out) >= maxCount {
			break
		}
	}
	return strings.Join(out, "\n"), nil
}

func init() {
	Register(ToolHolidays{})
}

// helper privado para iCal
func loadCalendar(ctx context.Context, url string) ([]*ics.VEvent, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, errors.New(fmt.Sprintf("calendar http %d", resp.StatusCode))
	}
	cal, err := ics.ParseCalendar(resp.Body)
	if err != nil {
		return nil, err
	}
	return cal.Events(), nil
}
