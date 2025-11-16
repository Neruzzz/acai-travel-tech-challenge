package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type ToolExchangeRate struct{}

func (ToolExchangeRate) Name() string { return "get_exchange_rate" }

func (ToolExchangeRate) Description() string {
	return "Get the latest FX rate or convert an amount between two currencies (ISO 4217 codes, e.g., EUR, USD). Powered by frankfurter.app, no API key required."
}

func (ToolExchangeRate) ParametersSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"base": map[string]any{
				"type":        "string",
				"description": "Base currency code (ISO 4217), e.g., EUR",
			},
			"symbol": map[string]any{
				"type":        "string",
				"description": "Target currency code (ISO 4217), e.g., USD",
			},
			"amount": map[string]any{
				"type":        "number",
				"description": "Optional amount to convert. If omitted, returns only the rate.",
			},
		},
		"required": []string{"base", "symbol"},
	}
}

var httpClientFX = &http.Client{Timeout: 10 * time.Second}

func (ToolExchangeRate) Call(ctx context.Context, args map[string]any) (string, error) {
	baseRaw, _ := args["base"].(string)
	symbolRaw, _ := args["symbol"].(string)
	amount, _ := args["amount"].(float64) // optional

	base := strings.ToUpper(strings.TrimSpace(baseRaw))
	symbol := strings.ToUpper(strings.TrimSpace(symbolRaw))

	if base == "" || symbol == "" {
		return "", errors.New("missing 'base' or 'symbol'")
	}
	if len(base) != 3 || len(symbol) != 3 {
		return "", errors.New("currency codes must be ISO 4217 (3 letters)")
	}
	if amount < 0 {
		return "", errors.New("amount must be >= 0")
	}

	u := fmt.Sprintf("https://api.frankfurter.app/latest?from=%s&to=%s",
		url.QueryEscape(base), url.QueryEscape(symbol))

	slog.InfoContext(ctx, "FX request", "base", base, "symbol", symbol, "url", u)
	body, status, err := httpGET(ctx, u)
	if err != nil {
		return "", err
	}
	if status >= 400 {
		return "", fmt.Errorf("frankfurter http %d: %s", status, body)
	}

	var p struct {
		Amount float64            `json:"amount"`
		Base   string             `json:"base"`
		Date   string             `json:"date"`
		Rates  map[string]float64 `json:"rates"`
	}
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		return "", fmt.Errorf("decode error: %w (body=%s)", err, body)
	}

	val := p.Rates[symbol]
	if val == 0 {
		return "", fmt.Errorf("rate not found for %s (body=%s)", symbol, body)
	}

	slog.InfoContext(ctx, "FX provider OK",
		"provider", "frankfurter.app",
		"base", base,
		"symbol", symbol,
		"rate", val,
		"date", p.Date,
	)

	out := map[string]any{
		"provider": "frankfurter.app",
		"base":     base,
		"symbol":   symbol,
		"rate":     val,
		"date":     p.Date,
	}
	if amount > 0 {
		out["amount"] = amount
		out["converted"] = amount * val
	}
	b, _ := json.Marshal(out)
	return string(b), nil
}

func httpGET(ctx context.Context, u string) (body string, status int, err error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	req.Header.Set("User-Agent", "acai-challenge/1.0 (+github.com/Neruzzz)")
	req.Header.Set("Accept", "application/json")

	resp, err := httpClientFX.Do(req)
	if err != nil {
		slog.ErrorContext(ctx, "HTTP error", "url", u, "err", err)
		return "", 0, err
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return string(b), resp.StatusCode, nil
}

func init() { Register(ToolExchangeRate{}) }
