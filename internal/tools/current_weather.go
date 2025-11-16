package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
)

type ToolCurrentWeather struct{}

func (ToolCurrentWeather) Name() string { return "get_current_weather" }

func (ToolCurrentWeather) Description() string {
	return "Get current weather for a given location. Returns temperature, wind, humidity, condition, etc."
}

func (ToolCurrentWeather) ParametersSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"location": map[string]any{
				"type":        "string",
				"description": "City name or 'lat,lon' coordinates",
			},
		},
		"required": []string{"location"},
	}
}

func (ToolCurrentWeather) Call(ctx context.Context, args map[string]any) (string, error) {
	loc, _ := args["location"].(string)
	if loc == "" {
		return "", errors.New("missing 'location'")
	}

	apiKey := os.Getenv("WEATHER_API_KEY")
	if apiKey == "" {
		return "", errors.New("missing WEATHER_API_KEY")
	}

	u := "https://api.weatherapi.com/v1/current.json?key=" + url.QueryEscape(apiKey) + "&q=" + url.QueryEscape(loc)
	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("weather api http %d", resp.StatusCode)
	}

	var payload struct {
		Location struct {
			Name    string  `json:"name"`
			Region  string  `json:"region"`
			Country string  `json:"country"`
			Lat     float64 `json:"lat"`
			Lon     float64 `json:"lon"`
			TzID    string  `json:"tz_id"`
		} `json:"location"`
		Current struct {
			TempC     float64 `json:"temp_c"`
			WindKph   float64 `json:"wind_kph"`
			WindDir   string  `json:"wind_dir"`
			GustKph   float64 `json:"gust_kph"`
			Humidity  int     `json:"humidity"`
			FeelsLike float64 `json:"feelslike_c"`
			PrecipMm  float64 `json:"precip_mm"`
			Pressure  float64 `json:"pressure_mb"`
			Cloud     int     `json:"cloud"`
			UV        float64 `json:"uv"`
			VisKm     float64 `json:"vis_km"`
			Condition struct {
				Text string `json:"text"`
			} `json:"condition"`
		} `json:"current"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}

	out, _ := json.Marshal(map[string]any{
		"resolved_name": fmt.Sprintf("%s, %s, %s", payload.Location.Name, payload.Location.Region, payload.Location.Country),
		"coords":        []float64{payload.Location.Lat, payload.Location.Lon},
		"timezone":      payload.Location.TzID,
		"temperature_c": payload.Current.TempC,
		"wind_kph":      payload.Current.WindKph,
		"wind_dir":      payload.Current.WindDir,
		"gust_kph":      payload.Current.GustKph,
		"humidity":      payload.Current.Humidity,
		"feelslike_c":   payload.Current.FeelsLike,
		"precip_mm":     payload.Current.PrecipMm,
		"pressure_mb":   payload.Current.Pressure,
		"cloud":         payload.Current.Cloud,
		"uv":            payload.Current.UV,
		"vis_km":        payload.Current.VisKm,
		"condition":     payload.Current.Condition.Text,
	})
	return string(out), nil
}

func init() {
	Register(ToolCurrentWeather{})
}
