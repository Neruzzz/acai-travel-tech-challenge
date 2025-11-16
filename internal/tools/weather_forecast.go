package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type DailyForecast struct {
	Date          string  `json:"date"`
	MaxTempC      float64 `json:"max_temp_c"`
	MinTempC      float64 `json:"min_temp_c"`
	Condition     string  `json:"condition"`
	ChanceOfRain  int     `json:"chance_of_rain"`
	TotalPrecipMm float64 `json:"total_precip_mm"`
	MaxWindKph    float64 `json:"max_wind_kph"`
	UV            float64 `json:"uv"`
	Sunrise       string  `json:"sunrise"`
	Sunset        string  `json:"sunset"`
}

var httpClientForecast = &http.Client{Timeout: 8 * time.Second}

type ToolWeatherForecast struct{}

func (ToolWeatherForecast) Name() string { return "get_weather_forecast" }

func (ToolWeatherForecast) Description() string {
	return "Provides a multi-day weather forecast (up to 7 days) for a given location."
}

func (ToolWeatherForecast) ParametersSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"location": map[string]any{
				"type":        "string",
				"description": "City name or coordinates (lat,lon) to get the weather forecast for.",
			},
			"days": map[string]any{
				"type":        "integer",
				"description": "Number of days to forecast (1-7).",
				"minimum":     1,
				"maximum":     7,
			},
		},
		"required": []string{"location"},
	}
}

func (ToolWeatherForecast) Call(ctx context.Context, args map[string]any) (string, error) {
	location, _ := args["location"].(string)
	days, _ := args["days"].(float64)
	if location == "" {
		return "", errors.New("missing location parameter")
	}
	if days <= 0 {
		days = 3
	}
	if days > 7 {
		days = 7
	}

	apiKey := strings.TrimSpace(os.Getenv("WEATHER_API_KEY"))
	if apiKey == "" {
		return "", errors.New("missing WEATHER_API_KEY environment variable")
	}

	endpoint := fmt.Sprintf(
		"https://api.weatherapi.com/v1/forecast.json?key=%s&q=%s&days=%d&aqi=no&alerts=no",
		url.QueryEscape(apiKey),
		url.QueryEscape(location),
		int(days),
	)

	req, _ := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	res, err := httpClientForecast.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	if res.StatusCode >= 400 {
		var e struct {
			Error struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.NewDecoder(res.Body).Decode(&e)
		if e.Error.Message != "" {
			return "", fmt.Errorf("weatherapi error: %s (code %d)", e.Error.Message, e.Error.Code)
		}
		return "", fmt.Errorf("weatherapi http %d", res.StatusCode)
	}

	var payload struct {
		Forecast struct {
			Forecastday []struct {
				Date string `json:"date"`
				Day  struct {
					MaxtempC  float64 `json:"maxtemp_c"`
					MintempC  float64 `json:"mintemp_c"`
					Condition struct {
						Text string `json:"text"`
					} `json:"condition"`
					DailyChanceOfRain int     `json:"daily_chance_of_rain"`
					TotalPrecipMm     float64 `json:"totalprecip_mm"`
					MaxwindKph        float64 `json:"maxwind_kph"`
					UV                float64 `json:"uv"`
				} `json:"day"`
				Astro struct {
					Sunrise string `json:"sunrise"`
					Sunset  string `json:"sunset"`
				} `json:"astro"`
			} `json:"forecastday"`
		} `json:"forecast"`
	}

	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return "", err
	}

	out := make([]DailyForecast, 0, len(payload.Forecast.Forecastday))
	for _, d := range payload.Forecast.Forecastday {
		out = append(out, DailyForecast{
			Date:          d.Date,
			MaxTempC:      d.Day.MaxtempC,
			MinTempC:      d.Day.MintempC,
			Condition:     d.Day.Condition.Text,
			ChanceOfRain:  d.Day.DailyChanceOfRain,
			TotalPrecipMm: d.Day.TotalPrecipMm,
			MaxWindKph:    d.Day.MaxwindKph,
			UV:            d.Day.UV,
			Sunrise:       d.Astro.Sunrise,
			Sunset:        d.Astro.Sunset,
		})
	}

	bytes, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func init() {
	Register(ToolWeatherForecast{})
}
