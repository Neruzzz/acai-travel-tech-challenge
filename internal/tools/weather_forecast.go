package weather

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
	Date          string
	MaxTempC      float64
	MinTempC      float64
	Condition     string
	ChanceOfRain  int
	TotalPrecipMm float64
	MaxWindKph    float64
	UV            float64
	Sunrise       string
	Sunset        string
}

var httpClientForecast = &http.Client{Timeout: 8 * time.Second}

func GetForecast(ctx context.Context, location string, days int) ([]DailyForecast, error) {
	apiKey := strings.TrimSpace(os.Getenv("WEATHER_API_KEY"))
	if apiKey == "" {
		return nil, errors.New("missing WEATHER_API_KEY")
	}
	if strings.TrimSpace(location) == "" {
		return nil, errors.New("empty location")
	}
	if days <= 0 {
		days = 3
	}
	if days > 7 {
		days = 7
	}

	endpoint := fmt.Sprintf(
		"https://api.weatherapi.com/v1/forecast.json?key=%s&q=%s&days=%d&aqi=no&alerts=no",
		url.QueryEscape(apiKey),
		url.QueryEscape(location),
		days,
	)

	req, _ := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	res, err := httpClientForecast.Do(req)
	if err != nil {
		return nil, err
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
			return nil, fmt.Errorf("weatherapi error: %s (code %d)", e.Error.Message, e.Error.Code)
		}
		return nil, fmt.Errorf("weatherapi http %d", res.StatusCode)
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
		return nil, err
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
	return out, nil
}
