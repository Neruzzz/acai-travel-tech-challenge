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

type CurrentReport struct {
    ResolvedName string
    Latitude     float64
    Longitude    float64
    TemperatureC float64
    WindKph      float64
    WindDir      string
    GustKph      float64
    Humidity     int
    FeelsLikeC   float64
    PrecipMm     float64
    PressureMb   float64
    Cloud        int
    UV           float64
    VisKm        float64
    Condition    string
}

var httpClient = &http.Client{Timeout: 8 * time.Second}

func GetCurrent(ctx context.Context, location string) (*CurrentReport, error) {
	apiKey := strings.TrimSpace(os.Getenv("WEATHER_API_KEY"))
	if apiKey == "" {
		return nil, errors.New("missing WEATHER_API_KEY")
	}
	if strings.TrimSpace(location) == "" {
		return nil, errors.New("empty location")
	}

	endpoint := fmt.Sprintf(
		"https://api.weatherapi.com/v1/current.json?key=%s&q=%s&aqi=no",
		url.QueryEscape(apiKey),
		url.QueryEscape(location),
	)

	req, _ := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	res, err := httpClient.Do(req)
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
		Location struct {
			Name    string  `json:"name"`
			Region  string  `json:"region"`
			Country string  `json:"country"`
			Lat     float64 `json:"lat"`
			Lon     float64 `json:"lon"`
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

	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return nil, err
	}

	name := payload.Location.Name
	if payload.Location.Region != "" {
		name = fmt.Sprintf("%s, %s", name, payload.Location.Region)
	}
	if payload.Location.Country != "" {
		name = fmt.Sprintf("%s, %s", name, payload.Location.Country)
	}

	return &CurrentReport{
		ResolvedName: name,
		Latitude:     payload.Location.Lat,
		Longitude:    payload.Location.Lon,
		TemperatureC: payload.Current.TempC,
		WindKph:      payload.Current.WindKph,
		WindDir:      payload.Current.WindDir,
		GustKph:      payload.Current.GustKph,
		Humidity:     payload.Current.Humidity,
		FeelsLikeC:   payload.Current.FeelsLike,
		PrecipMm:     payload.Current.PrecipMm,
		PressureMb:   payload.Current.Pressure,
		Cloud:        payload.Current.Cloud,
		UV:           payload.Current.UV,
		VisKm:        payload.Current.VisKm,
		Condition:    payload.Current.Condition.Text,
	}, nil
}
