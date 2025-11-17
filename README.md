# Technical challenge documentation by Imanol Rojas
## First steps
- I modified the Makefile such that the contents of the environment file is exported at runtime. This automates the projet deployment making it scalable to new tokens and environment variables.
- I moved Acai documentation to the `doc`folder so the reviewer sees my documentation first. The original documentation can be found [here](/doc/README.md)
- Finally I installed go, and executed the application to check that the repo works as expected.

## Task 1 – Fix conversation title
### Problem

The original Title implementation attempted to use OpenAI to generate a conversation title, but the way the prompt was built caused incorrect behavior:

```go
msgs := make([]openai.ChatCompletionMessageParamUnion, len(conv.Messages))

msgs[0] = openai.AssistantMessage("Generate a concise, descriptive title...")
for i, m := range conv.Messages {
    msgs[i] = openai.UserMessage(m.Content)
}
```

Issues:

- The instruction message set at msgs[0] is overwritten in the for loop.
- All conversation messages are sent as user messages, without a proper system role and system message.
- The model receives the full conversation without explicit rules, so it behaves as a regular assistant and answers the question instead of generating a title.


### Solution

Key design decisions:

- Use the first user message as the source for the title.
- Send a dedicated system message that explicitly defines the expected output.
- Limit the response to a short noun phrase, not an answer.
- Add a safe fallback title if the model response is empty or an error occurs.


The Title method was updated to:

- Extract the first non-empty user message from the conversation.
- Build a minimal prompt with:
    - A system message describing how to format the title.
    - A user message containing only the first user message.
- Call OpenAI with this controlled prompt.
- Fallback to "New conversation" when the response is invalid.
- Post-process the result (trim spaces, remove quotes/newlines, enforce a max length).
- Fallback to "New conversation" when the response after the post-process is empty. invalid.

Snippet of the updated logic:

```go
// 1. Take the first meaningful user message as input.
var firstUserMessage string
for _, m := range conv.Messages {
    if m.Role == model.RoleUser && strings.TrimSpace(m.Content) != "" {
        firstUserMessage = m.Content
        break
    }
}
if firstUserMessage == "" {
    firstUserMessage = conv.Messages[0].Content
}

// 2. Instruct the model explicitly via system message and set the user message
system := openai.SystemMessage(`You generate concise conversation titles.

Rules:
- Output ONLY a short noun phrase summarizing the user's first message.
- Do NOT answer the question.
- Do NOT include quotes.
- Maximum 6 words.`)

user := openai.UserMessage(firstUserMessage)

// 3. Ask OpenAI using [system, user] messages only.
resp, err := a.cli.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
    Model:    openai.ChatModelGPT4_1,
    Messages: []openai.ChatCompletionMessageParamUnion{system, user},
})

// 4. Clean and validate the returned title; fallback if needed.
if err != nil || len(resp.Choices) == 0 {
    return "New conversation", nil
}

title := resp.Choices[0].Message.Content
title = strings.ReplaceAll(title, "\n", " ")
title = strings.Trim(title, " \t\r\n-\"'")

if title == "" {
    return "New conversation", nil
}

if len(title) > 80 {
    title = title[:80]
}
```

### Result

Conversation titles after the fix:

```text
ID                         TITLE
69138ad828032f0e80507808   Advanced LLMs for Programming
69138a7928032f0e80507805   Go programming language syntax overview
69137c4c28032f0e80507802   Barcelona current weather information
```

## Task 1 – Bonus: Optimize `StartConversation()` performance
### Problem
The StartConversation endpoint executed two sequential (while independent) API calls to OpenAI:

```go
// choose a title
title, err := s.assist.Title(ctx, conversation)
if err != nil {
    slog.ErrorContext(ctx, "Failed to generate conversation title", "error", err)
} else {
    conversation.Title = title
}

// generate a reply
reply, err := s.assist.Reply(ctx, conversation)
if err != nil {
    return nil, err
}
```

Both methods perform independent remote requests:

- Title generates a short summary of the user’s message.
- Reply generates the assistant’s actual response.

Since both depend only on the initial user message and not on each other, running them sequentially generates unnecessary latency: `total time ≈ title_time + reply_time`.

### Solution

Key design decisions:

- Run title and reply generation concurrently using goroutines.
- Use channels to collect both results safely, avoiding data races.
- Preserve identical external behavior:
    - If Title fails, use "Untitled conversation" as fallback.
    - If Reply fails, return an internal error as before.
- Store the final conversation once both operations have completed.

Snippet of the updated logic:

```go
// Create a channel for each operation
titleCh := make(chan string, 1)
replyCh := make(chan struct {
    val string
    err error
}, 1)

// Run title generation in parallel
go func() {
    title, err := s.assist.Title(ctx, conversation)
    if err != nil || strings.TrimSpace(title) == "" {
        slog.ErrorContext(ctx, "Failed to generate conversation title", "error", err)
        titleCh <- "Untitled conversation"
        return
    }
    titleCh <- title
}()

// Run reply generation in parallel
go func() {
    reply, err := s.assist.Reply(ctx, conversation)
    replyCh <- struct {
        val string
        err error
    }{val: reply, err: err}
}()

// Wait for both results
title := <-titleCh
replyResult := <-replyCh
if replyResult.err != nil {
    return nil, twirp.InternalErrorWith(replyResult.err)
}
reply := replyResult.val

conversation.Title = title

```

### Result

Now the interaction with OpenAI for the title and the respoinse is done at the same time, this can be seen on the server's console:

```text
2025/11/11 21:36:39 INFO Starting the server...
2025/11/11 21:37:09 INFO Generating reply for conversation conversation_id=69139e750192fa6b9b0b3f50
2025/11/11 21:37:09 INFO Generating title for conversation conversation_id=69139e750192fa6b9b0b3f50
2025/11/11 21:37:11 INFO HTTP request complete http_method=POST http_path=/twirp/acai.chat.ChatService/StartConversation http_status=200
```
Both the generation of the title and the response start at the sema time

- Before: two blocking network calls → `total latency ≈ title + reply`
- After: concurrent execution → `total latency ≈ max(title, reply)`

This change improves responsiveness while keeping the same functionality and database flow intact.

## Task 2 – Fix the weather
### Problem

The existing `get_weather` tool in `assistant.go` did not fetch real weather information. Instead, it returned a static placeholder:

```go
case "get_weather":
    msgs = append(msgs, openai.ToolMessage("weather is fine", call.ID))
```

Issues:

- No external API integration, responses were always "the weather is fine".
- The assistant could not display real-time temperature, wind, or conditions.
- No differentiation between brief vs. detailed weather queries.
- No error handling or validation for invalid locations or missing API keys.

### Solution

Key design decisions:

- Replace the placeholder with a real integration using the public WeatherAPI
- Create a new package [internal/tools/current_weather.go](internal/tools/current_weather.go) to encapsulate all HTTP calls, parsing, and error handling.
- Fetch and return live weather data: temperature, wind, humidity, pressure, UV index, and precipitation.
- Extend the tool’s JSON schema with a new details flag so the LLM can request compact or detailed output.
- Support missing/invalid keys gracefully by returning a clear "failed to fetch weather" message.
- Keep the output simple and human-readable for chat responses.

### Implementation

I first implemented the weather tool to get a reduced report, then I extended it so it can get more parameters in case the user asks for more details. It has two modes, at first it will give a summary of the weather and when asked for more details the query will return a more sophisticated report. The implementation is as follows:

#### 1. New weather package

A new helper file [`internal/tools/weather.go`](internal/tools/weather.go) defines the `GetCurrent` function which interacts with WeatherAPI to get the weather report:

```go
func GetCurrent(ctx context.Context, location string) (*CurrentReport, error)
```

This function:

- Reads `WEATHER_API_KEY` from the environment.
- Calls https://api.weatherapi.com/v1/current.json.
- Parses relevant fields into a strongly-typed struct.

Report structure:

```go
type CurrentReport struct {
    ResolvedName string
    TemperatureC float64
    FeelsLikeC   float64
    WindKph      float64
    WindDir      string
    Humidity     int
    PrecipMm     float64
    PressureMb   float64
    Cloud        int
    UV           float64
    VisKm        float64
    Condition    string
}
```

This isolates all WeatherAPI logic from the assistant core and makes it reusable for other tasks.

#### 2. Updated assistant logic

The `get_weather` tool in `assistant.go` was replaced with a real call to `weather.GetCurrent`. The function now unmarshals (parses) the model’s arguments which are the location, and if the user asked for details:

```go
var args struct {
    Location string `json:"location"`
    Details  bool   `json:"details,omitempty"`
}
```

Then builds a formatted response which includes extra information depending on the user query (`Details`):

```go
rep, err := weather.GetCurrent(ctx, args.Location)
if err != nil {
    msgs = append(msgs, openai.ToolMessage("failed to fetch weather", call.ID))
    break
}

var b strings.Builder
fmt.Fprintf(&b, "Location: %s\n", rep.ResolvedName)
fmt.Fprintf(&b, "Current: %.1f°C (feels %.1f°C), %s\n", rep.TemperatureC, rep.FeelsLikeC, rep.Condition)
fmt.Fprintf(&b, "Wind: %.1f km/h %s\n", rep.WindKph, rep.WindDir)

if args.Details {
    fmt.Fprintf(&b, "Humidity: %d%% | Cloud: %d%% | UV: %.1f\n", rep.Humidity, rep.Cloud, rep.UV)
    fmt.Fprintf(&b, "Pressure: %.0f mb | Visibility: %.1f km | Precip: %.2f mm\n",
        rep.PressureMb, rep.VisKm, rep.PrecipMm)
}

msgs = append(msgs, openai.ToolMessage(b.String(), call.ID))
```

### Result

In the next conversation thare are two examples of the functionality of `GetCurrent`. One is the basic question with the weather summary as a response, and the next round is the query for more details from the user and the full report answer from the assistant.

```text
USER:
What is the weather in Bangkok?

New conversation started:
ID: 6914eafef885c0ccc206c958
Title: Bangkok current weather information

ASSISTANT:
The current weather in Bangkok is 26.2°C (feels like 29.3°C) with moderate or heavy rain and thunder. Winds are coming from the SSE at 5.4 km/h.

USER:
I want all the weather data you can give me  

ASSISTANT:
Here is the detailed current weather for Bangkok:

- Temperature: 26.2°C (feels like 29.3°C)
- Conditions: Moderate or heavy rain with thunder
- Wind: 5.4 km/h from SSE
- Humidity: 84%
- Cloud Cover: 75%
- UV Index: 0.0
- Pressure: 1011 mb
- Visibility: 10.0 km
- Precipitation: 0.01 mm

Let me know if you need a forecast or more details!
```

## Task 2 – Bonus: Weather forecast

### Problem

The assistant originally handled **only current weather**. Users requesting **tomorrow**, **weekend**, or **multi-day** forecasts received incomplete answers because there was no mechanism to fetch future weather (max/min temperatures, chance of rain, precipitation totals, UV index, sunrise/sunset, etc.).

The system needed a clean way to request forecast data and a consistent output format matching the style of the current-weather tool.

---

### Solution

Key design decisions:

- Add a **new tool**, `get_forecast`, dedicated to multi-day forecast queries.
- Use the same WeatherAPI backend, but through a new helper function `GetForecast`.
- Support **1–7 days**. If the request is outside the range, clamp it to the valid bounds.
- Support both:
  - concise output (`details=false`)  
  - enriched output (`details=true`)
- Reuse the same formatting patterns used in the current-weather tool for consistency.

This approach extends weather functionality without changing the behavior of the original `get_weather` tool.


### Implementation

The forecast logic is split in two main parts:


#### 1. Forecast helper (`GetForecast`)

**File:** `internal/tools/weather/weather_forecast.go`  
**Function:** `GetForecast(ctx, location, days)`

Responsibilities:

- Make an HTTP call to:  
  `https://api.weatherapi.com/v1/forecast.json`
- Clamp the number of days to `[1..7]`.
- Parse JSON cleanly into a Go struct rather than working with raw maps.
- Return a slice of `DailyForecast` ready for formatting.

Structure (report) definition:

```go
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
```
Core logic:

```go
func GetForecast(ctx context.Context, location string, days int) ([]DailyForecast, error) {
    apiKey := strings.TrimSpace(os.Getenv("WEATHER_API_KEY"))
    if apiKey == "" {
        return nil, errors.New("missing WEATHER_API_KEY")
    }
    if strings.TrimSpace(location) == "" {
        return nil, errors.New("empty location")
    }

    if days <= 0 { days = 3 }
    if days > 7 { days = 7 }

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

    // parse JSON...
}

```

The function returns a slice of `DailyForecast` containing all relevant fields.

#### 2. The new `get_forecast` tool in `assistant.go`

The tool is defined in the same format as the existing tools in the assistant:

```go
openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
    Name:        "get_forecast",
    Description: openai.String("Get daily weather forecast for the next N days at a given location."),
    Parameters: openai.FunctionParameters{
        "type": "object",
        "properties": map[string]any{
            "location": map[string]string{"type": "string"},
            "days": map[string]any{
                "type": "integer", "minimum": 1, "maximum": 7,
                "description": "How many days ahead (1–7). Defaults to 3.",
            },
            "details": map[string]string{
                "type":        "boolean",
                "description": "If true, include precip totals, UV, wind, sunrise/sunset.",
            },
        },
        "required": []string{"location"},
    },
})
```

#### 3. Handling the tool call in assistant.go

When the model calls `get_forecast`, the assistant parses the input, fetches the forecast and finally formats the output:

```go
case "get_forecast":
    var args struct {
        Location string `json:"location"`
        Days     int    `json:"days,omitempty"`
        Details  bool   `json:"details,omitempty"`
    }
    if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil || strings.TrimSpace(args.Location) == "" {
        msgs = append(msgs, openai.ToolMessage("failed to parse arguments for get_forecast", call.ID))
        break
    }

    df, err := weather.GetForecast(ctx, args.Location, args.Days)
    if err != nil {
        msgs = append(msgs, openai.ToolMessage("failed to fetch forecast", call.ID))
        break
    }

    var b strings.Builder
    fmt.Fprintf(&b, "Forecast for %s:\n", args.Location)
    for _, d := range df {
        if args.Details {
            fmt.Fprintf(&b, "- %s: %s, %.0f/%.0f°C, rain %d%%, precip %.1f mm, wind %.0f km/h, UV %.1f, sunrise %s, sunset %s\n",
                d.Date, d.Condition, d.MaxTempC, d.MinTempC, d.ChanceOfRain, d.TotalPrecipMm, d.MaxWindKph, d.UV, d.Sunrise, d.Sunset)
        } else {
            fmt.Fprintf(&b, "- %s: %s, %.0f/%.0f°C, rain %d%%\n",
                d.Date, d.Condition, d.MaxTempC, d.MinTempC, d.ChanceOfRain)
        }
    }
    msgs = append(msgs, openai.ToolMessage(b.String(), call.ID))

```

### Result

Here are several examples of the implemented tool working:

```text
Starting a new conversation, type your message below.

USER:
Give me the weather for this week in Barcelona 

New conversation started:
ID: 6915fde7984742ca45cf088f
Title: Barcelona weekly weather forecast

ASSISTANT:
Here is the weather forecast for Barcelona this week:

- Nov 13: Patchy rain, 20/17°C, very little rain
- Nov 14: Patchy rain, 21/17°C, likely rain (3.5 mm)
- Nov 15: Patchy rain, 20/16°C, some rain (0.8 mm)
- Nov 16: Patchy rain, 19/17°C, likely rain (4.2 mm)
- Nov 17: Patchy rain, 18/14°C, light rain
- Nov 18: Patchy rain, 15/13°C, almost dry
- Nov 19: Partly cloudy, 12/11°C, dry

Expect mild temperatures with frequent chances of rain, especially midweek. The weather will improve toward the end of the week.

USER:
I want it will all the details you can give me

ASSISTANT:
Here’s a detailed 7-day weather forecast for Barcelona:

November 13
- Weather: Patchy rain nearby
- Temperatures: High 20°C / Low 17°C
- Precipitation: 0.1 mm (Rain chance: 0%)
- Wind: 24 km/h
- UV Index: 0.4 (Low)
- Sunrise: 07:38 AM / Sunset: 05:34 PM

November 14
- Weather: Patchy rain nearby
- Temperatures: 21°C / 17°C
- Precipitation: 3.5 mm (Rain chance: 87%)
- Wind: 27 km/h
- UV Index: 0.3 (Low)
- Sunrise: 07:39 AM / Sunset: 05:33 PM

November 15
- Weather: Patchy rain nearby
- Temperatures: 20°C / 16°C
- Precipitation: 0.8 mm (Rain chance: 86%)
- Wind: 27 km/h
- UV Index: 0.4 (Low)
- Sunrise: 07:40 AM / Sunset: 05:32 PM

November 16
- Weather: Patchy rain nearby
- Temperatures: 19°C / 17°C
- Precipitation: 4.2 mm (Rain chance: 89%)
- Wind: 21 km/h
- UV Index: 0.4 (Low)
- Sunrise: 07:41 AM / Sunset: 05:31 PM

November 17
- Weather: Patchy rain nearby
- Temperatures: 18°C / 14°C
- Precipitation: 0.3 mm (Rain chance: 81%)
- Wind: 13 km/h
- UV Index: 0.4 (Low)
- Sunrise: 07:42 AM / Sunset: 05:30 PM

November 18
- Weather: Patchy rain nearby
- Temperatures: 15°C / 13°C
- Precipitation: 0.1 mm (Rain chance: 0%)
- Wind: 13 km/h
- UV Index: 3.0 (Moderate)
- Sunrise: 07:44 AM / Sunset: 05:29 PM

November 19
- Weather: Partly Cloudy
- Temperatures: 12°C / 11°C
- Precipitation: 0.0 mm (Rain chance: 0%)
- Wind: 13 km/h
- UV Index: 4.0 (Moderate)
- Sunrise: 07:45 AM / Sunset: 05:29 PM

Expect mild to cool temperatures, mostly cloudy skies, and several days of rain early in the week. The weather will dry out and clear up somewhat toward the end of the week.
```

If asked for a determined number of days:

```text
USER:
And the weather in Bangkok for the next 3 days?    

ASSISTANT:
Here’s the detailed weather forecast for Bangkok for the next 3 days:

November 13
- Weather: Heavy rain
- Temperatures: High 30°C / Low 26°C
- Precipitation: 21.5 mm (rain chance: 97%)
- Wind: 9 km/h
- UV Index: 1.7 (Low)
- Sunrise: 06:17 AM / Sunset: 05:48 PM

November 14
- Weather: Moderate rain
- Temperatures: 29°C / 25°C
- Precipitation: 12.4 mm (rain chance: 93%)
- Wind: 9 km/h
- UV Index: 1.8 (Low)
- Sunrise: 06:17 AM / Sunset: 05:48 PM

November 15
- Weather: Sunny
- Temperatures: 30°C / 23°C
- Precipitation: 0.0 mm (rain chance: 0%)
- Wind: 13 km/h
- UV Index: 1.9 (Low)
- Sunrise: 06:17 AM / Sunset: 05:48 PM

Expect rain the next two days, with improving weather and sunshine on the third day.
```

## Task 3

### Problem

Originally, all tools were hard-coded and coupled to the `assistant.go` file. This led to several issues:

- There was no common contract, each tool had different signatures and inconsistent schemas which had to be defined on the main logic of the assistant.
- Adding or modifying tools required editing assistant.go, increasing the risk of regressions and low scalability.
- There was no dynamic discovery, if a tool wasn’t manually wired, the assistant silently ignored it.
- No structured logging made it hard to debug when a tool was called or failed.

### Solution

Key dessign decissions:

- Define a unified Tool interface shared by every tool.
- Introduce a registry to dynamically register and discover available tools.
- Split each tool into its own file inside `internal/tools/` for modularity.
- Make `assistant.go` responsible only for:
    - Dynamically exposing tools to the model.
    - Routing tool calls.
    - Handling execution results.
- Improve observability with logs.

### Implementation

#### Tool Structure

All tools now implement the following interface (in [`internal/tools/registry.go`](internal/tools/registry.go)):

```go
type Tool interface {
    Name() string
    Description() string
    ParametersSchema() map[string]any
    Call(ctx context.Context, args map[string]any) (string, error)
}
```

- `Name()`: exact tool identifier (must match what the model will call).

- `Description()`: short text explaining the tool’s purpose.

- `ParametersSchema()`: JSON Schema describing accepted arguments. This allows OpenAI to validate and auto-generate correct calls.

- `Call()`: executes the logic, receives parsed args, and returns a string (usually JSON).

Each tool registers itself automatically via `init()`:

```go
func init() {
    Register(ToolCurrentWeather{})
}
```

Example `internal/tools/current_weather.go`:

```go
type ToolCurrentWeather struct{}

func (ToolCurrentWeather) Name() string { return "get_current_weather" }

func (ToolCurrentWeather) Description() string {
    return "Get current weather for a given location."
}

func (ToolCurrentWeather) ParametersSchema() map[string]any {
    return map[string]any{
        "type": "object",
        "properties": map[string]any{
            "location": map[string]any{
                "type": "string",
                "description": "City name or coordinates.",
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
    // Fetch data, format output...
    return `{"location": "Barcelona", "temp_c": 21.4}`, nil // Example output
}

func init() { Register(ToolCurrentWeather{}) } // init for registering the tool

```

#### The role of `registry.go`

[`internal/tools/registry.go`](internal/tools/registry.go) is a lightweight global registry that tracks all available tools:

```go
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
```

Responsibilities:

- Provides a single source of truth for all registered tools.
- Makes tools discoverable dynamically (no hardcoding) and scalable.
- Enables the assistant to build its list of available tools and to route calls by name.

Design justification:

- Keeps the assistant core agnostic of how many or which tools exist.
- Adding a new tool becomes a single, isolated action (create file + `init()` registration).
- Prevents repetitive switch/case logic in assistant.go which is not scalable.
- Encourages loose coupling and open/closed design, new tools don’t require code changes elsewhere.

#### Changes in `assistant.go`

##### 1. Dynamic tool exposure

Instead of manually listing tools, the assistant now builds them dynamically from the registry:

```go
// Collect tools from registry
var toolDefs []openai.ChatCompletionToolUnionParam
for _, t := range tools.AllTools() {
    toolDefs = append(toolDefs, openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
        Name:        t.Name(),
        Description: openai.String(t.Description()),
        Parameters:  t.ParametersSchema(),
    }))
}
```

This list is passed to OpenAI when creating a new completion, ensuring the model knows exactly which tools it can use.

##### 2. Routing tool calls

When the model calls a tool, the assistant looks it up in the registry and executes it:

```go
for _, call := range choice.Message.ToolCalls {
    slog.InfoContext(ctx, "Tool call received",
        "name", call.Function.Name,
        "args", call.Function.Arguments,
    )

    tool := tools.FindByName(call.Function.Name)
    if tool == nil {
        slog.ErrorContext(ctx, "tool not found", "name", call.Function.Name)
        msgs = append(msgs, openai.ToolMessage("ERROR: tool not found", call.ID))
        continue
    }

    var args map[string]any
    if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
        slog.ErrorContext(ctx, "invalid args", "name", call.Function.Name, "err", err)
        msgs = append(msgs, openai.ToolMessage("ERROR: invalid tool arguments", call.ID))
        continue
    }

    res, err := tool.Call(ctx, args)
    if err != nil {
        slog.ErrorContext(ctx, "tool error", "name", call.Function.Name, "err", err)
        msgs = append(msgs, openai.ToolMessage(fmt.Sprintf("ERROR: %v", err), call.ID))
        continue
    }

    slog.InfoContext(ctx, "tool result", "name", call.Function.Name, "bytes", len(res))
    msgs = append(msgs, openai.ToolMessage(res, call.ID))
}
```

##### 3. Startup logging

On server startup, the assistant now automatically lists all registered tools from the central registry:

```go
func New() *Assistant {
    a := &Assistant{cli: openai.NewClient()}

    ts := tools.AllTools()
    if len(ts) == 0 {
        slog.Warn("No tools registered!")
    } else {
        slog.Info("Tools registered", "count", len(ts))
        for _, t := range ts {
            slog.Info("Tool registered", "name", t.Name(), "desc", t.Description())
        }
    }
    return a
}
```

This confirms that the assistant successfully loaded all tools before runtime. This is an example of the output:

```text
2025/11/16 20:48:13 INFO Tools registered count=4
2025/11/16 20:48:13 INFO Tool registered name=get_current_weather desc="Get current weather for a given location. Returns temperature, wind, humidity, condition, etc."
2025/11/16 20:48:13 INFO Tool registered name=get_holidays desc="Gets local bank and public holidays. Each line is 'YYYY-MM-DD: Holiday Name'."
2025/11/16 20:48:13 INFO Tool registered name=get_today_date desc="Get today's date and time in RFC3339 format."
2025/11/16 20:48:13 INFO Tool registered name=get_weather_forecast desc="Provides a multi-day weather forecast (up to 7 days) for a given location."
2025/11/16 20:48:13 INFO Starting the server...
2025/11/16 20:50:38 INFO Generating title for conversation conversation_id=691a2b0ea434c04ef8584406
2025/11/16 20:50:38 INFO Generating reply for conversation conversation_id=691a2b0ea434c04ef8584406
2025/11/16 20:50:40 INFO Tool call received name=get_today_date args={}
2025/11/16 20:50:41 INFO HTTP request complete http_method=POST http_path=/twirp/acai.chat.ChatService/StartConversation http_status=200
```

### Result

After the refactor:

- All tools (`get_today_date`, `get_holidays`, `get_current_weather`, `get_weather_forecast`, etc.) are automatically discovered and callable.
- The assistant’s logic is cleaner, more robust, and easier to extend.
- Adding a new capability now takes one new file + one `init()` line, without touching any shared logic.


## Task 3 – Bonus: Implement an extra tool. Exchange rates tool.

### Problem

The project required the implementation of a new, independent tool to demonstrate the extensibility of the tool registry system. The new tool:

- Is useful in conversational contexts (e.g., travel-related questions).
- Relies on a real external API.
- Works without any API keys to ensure reproducibility and easy setup.

### Solution

A new tool named `get_exchange_rate` was created.
It retrieves the latest currency exchange rate or performs a direct conversion between two currencies using the `frankfurter.app` API.

Key design decisions:

- No API key required, developer-friendly and reliable in all environments.
- Dynamic registration, integrated seamlessly into the tools registry with: `func init() { Register(ToolExchangeRate{}) }`
- Structured schema validation which ensures that the model provides correct parameters:
```json
{
  "base": "EUR",
  "symbol": "USD",
  "amount": 100
}
```
- Context-aware logging, every request and provider response is logged clearly.
- Automatic handling of both simple rate queries and full conversions with an optional amount field.

### Implementation

The tool implements the Tool interface in [`internal/tools/exchange_rate.go`](internal/tools/exchange_rate.go):

```go
type ToolExchangeRate struct{}

func (ToolExchangeRate) Name() string { return "get_exchange_rate" }

func (ToolExchangeRate) Description() string {
    return "Get the latest FX rate or convert an amount between two currencies (ISO 4217 codes, e.g., EUR, USD). Powered by frankfurter.app, no API key required."
}

func (ToolExchangeRate) ParametersSchema() map[string]any {
    return map[string]any{
        "type": "object",
        "properties": map[string]any{
            "base":   map[string]any{"type": "string", "description": "Base currency, e.g., EUR"},
            "symbol": map[string]any{"type": "string", "description": "Target currency, e.g., USD"},
            "amount": map[string]any{"type": "number", "description": "Optional amount to convert"},
        },
        "required": []string{"base", "symbol"},
    }
}
```

The Call method validates parameters, performs an HTTP request to Frankfurter, parses the JSON response, and returns a compact JSON result:

```go
u := fmt.Sprintf("https://api.frankfurter.app/latest?from=%s&to=%s", base, symbol)
slog.InfoContext(ctx, "FX request", "base", base, "symbol", symbol, "url", u)

resp, err := http.Get(u)
...
out := map[string]any{
    "provider": "frankfurter.app",
    "base":     base,
    "symbol":   symbol,
    "rate":     rate,
    "date":     date,
}
if amount > 0 {
    out["amount"] = amount
    out["converted"] = amount * rate
}
```

The tool is automatically discovered and loaded at server startup by the registry system in [`internal/tools/registry.go`](internal/tools/registry.go), which logs each registration:

```go
for _, t := range tools.AllTools() {
    slog.Info("Tool registered", "name", t.Name(), "desc", t.Description())
}
````

### Result
The server is able to register the tool and call it. The tool is able to provide a correct exchange rate.

```text
2025/11/16 21:28:12 INFO Tools registered count=5
2025/11/16 21:28:12 INFO Tool registered name=get_current_weather desc="Get current weather for a given location. Returns temperature, wind, humidity, condition, etc."
2025/11/16 21:28:12 INFO Tool registered name=get_exchange_rate desc="Get the latest FX rate or convert an amount between two currencies (ISO 4217 codes, e.g., EUR, USD). Powered by frankfurter.app, no API key required."
2025/11/16 21:28:12 INFO Tool registered name=get_holidays desc="Gets local bank and public holidays. Each line is 'YYYY-MM-DD: Holiday Name'."
2025/11/16 21:28:12 INFO Tool registered name=get_today_date desc="Get today's date and time in RFC3339 format."
2025/11/16 21:28:12 INFO Tool registered name=get_weather_forecast desc="Provides a multi-day weather forecast (up to 7 days) for a given location."
2025/11/16 21:28:12 INFO Starting the server...
2025/11/16 21:28:42 INFO Generating reply for conversation conversation_id=691a33fa19ed891738fac783
2025/11/16 21:28:42 INFO Generating title for conversation conversation_id=691a33fa19ed891738fac783
2025/11/16 21:28:42 INFO Tool call received name=get_exchange_rate args="{\"amount\":100,\"base\":\"EUR\",\"symbol\":\"THB\"}"
2025/11/16 21:28:42 INFO FX request base=EUR symbol=THB url="https://api.frankfurter.app/latest?from=EUR&to=THB"
2025/11/16 21:28:43 INFO FX provider OK provider=frankfurter.app base=EUR symbol=THB rate=37.74 date=2025-11-14
2025/11/16 21:28:43 INFO HTTP request complete http_method=POST http_path=/twirp/acai.chat.ChatService/StartConversation http_status=200
```

The assistant gives the infotmation to thje user as it should:

```text
Starting a new conversation, type your message below.

USER:
I want to know how mutch is 100 euros in thailand

New conversation started:
ID: 691a33fa19ed891738fac783
Title: Euro to Thai Baht Exchange Rate

ASSISTANT:
100 euros is approximately 3,774 Thai Baht (THB) at the current exchange rate.
```


## Task 4 – Create a test for StartConversation API
### Problem

We needed an automated test to ensure the `StartConversation` endpoint:

- Creates a new conversation (with ID and persisted state),
- Populates the title, and
- Triggers the assistant’s reply.

### Solution

- Introduced a fake assistant that implements the Assistant interface (Title, Reply) and returns predetermined strings. This makes `StartConversation` tests fast and stable.

- Wrote a new test in internal/chat/server_test.go that:
    - Calls StartConversation,
    - Asserts ID, title, and reply on the response,
    - Reads the conversation back via DescribeConversation to assert persistence and message roles (user/assistant).
- Added a negative test for empty message (expecting twirp.InvalidArgument).
- Kept the original DescribeConversation tests (existing behavior and 404).

### Implementation

Files & structure:

```text
internal/
  chat/
    server.go
    server_test.go        # StartConversation tests + existing DescribeConversation tests
    assistant/
      title_test.go       # Bonus: unit/integration tests for Assistant.Title
    testing/
      fixture.go          # Fixtures & helpers (unchanged)
      mongo.go            # Mongo helpers (unchanged)

```

The test is implemented in [`internal/chat/server_test.go`](internal/chat/server_test.go)

- Uses the project’s helpers: ConnectMongo, WithFixture, twirp.
- Adds:
    1. `TestServer_StartConversation_Creates_Populates_Triggers`
        - Asserts response (`ConversationId`, `Title`, `Reply`).
        - Calls `DescribeConversation` to verify title persisted and two messages exist with correct roles (`pb.Conversation_USER`, `pb.Conversation_ASSISTANT`) and assistant content.
    2. `TestServer_StartConversation_EmptyMessage_Err`
        - Asserts `twirp.InvalidArgument` for empty input.
- Keeps:
    3. `TestServer_DescribeConversation` (existing success + 404 cases).

### Result

How to run:

```bash
make up                 # start MongoDB for server tests
go test ./internal/chat -v
```

The output of the test is the following (all tests passed):

```text
trioteca:tech-challenge neruzz$ go test ./internal/chat -v
=== RUN   TestServer_StartConversation_Creates_Populates_Triggers
=== RUN   TestServer_StartConversation_Creates_Populates_Triggers/creates_conversation,_sets_title,_triggers_assistant_reply
--- PASS: TestServer_StartConversation_Creates_Populates_Triggers (0.03s)
    --- PASS: TestServer_StartConversation_Creates_Populates_Triggers/creates_conversation,_sets_title,_triggers_assistant_reply (0.03s)
=== RUN   TestServer_StartConversation_EmptyMessage_Err
=== RUN   TestServer_StartConversation_EmptyMessage_Err/empty_message_should_return_InvalidArgument
--- PASS: TestServer_StartConversation_EmptyMessage_Err (0.00s)
    --- PASS: TestServer_StartConversation_EmptyMessage_Err/empty_message_should_return_InvalidArgument (0.00s)
=== RUN   TestServer_DescribeConversation
=== RUN   TestServer_DescribeConversation/describe_existing_conversation
=== RUN   TestServer_DescribeConversation/describe_non_existing_conversation_should_return_404
--- PASS: TestServer_DescribeConversation (0.00s)
    --- PASS: TestServer_DescribeConversation/describe_existing_conversation (0.00s)
    --- PASS: TestServer_DescribeConversation/describe_non_existing_conversation_should_return_404 (0.00s)
PASS
ok      github.com/Neruzzz/acai-travel-challenge/internal/chat  0.408s
```

## Task 4 – Bonus: Tests for assistant.Title
### Problem

We need to develop a test for Assistant.Title (in `internal/chat/assistant/assistant.go`), without coupling to DB code or breaking local runs when OPENAI_API_KEY is absent.

### Solution

- Added a unit test for local logic (fallback when the conversation is empty).
- Added an optional integration test that calls the real model only if OPENAI_API_KEY is set; otherwise it skips.

### Implementation

The test is implemented in [`internal/chat/assistant/title_test.go`](internal/chat/assistant/title_test.go):

- Created:
    1. TestTitle_EmptyConversation_Fallback
        - No network, no DB. Asserts the function returns "An empty conversation" when there are no messages.
    2. TestTitle_GeneratesConciseTitle_Integration
        - Skips unless OPENAI_API_KEY is set.
        - Asserts non-empty, short, cleaned title (no quotes/newlines, not a question mark, length ≤ 80).

### Result

To run the test:

```bash
make up
go test ./internal/chat/assistant -v
```

The result of the test is below:

```text
trioteca:tech-challenge neruzz$ go test ./internal/chat/assistant -v
=== RUN   TestTitle_EmptyConversation_Fallback
2025/11/17 18:30:19 INFO Tools registered count=5
2025/11/17 18:30:19 INFO Tool registered name=get_current_weather desc="Get current weather for a given location. Returns temperature, wind, humidity, condition, etc."
2025/11/17 18:30:19 INFO Tool registered name=get_exchange_rate desc="Get the latest FX rate or convert an amount between two currencies (ISO 4217 codes, e.g., EUR, USD). Powered by frankfurter.app, no API key required."
2025/11/17 18:30:19 INFO Tool registered name=get_holidays desc="Gets local bank and public holidays. Each line is 'YYYY-MM-DD: Holiday Name'."
2025/11/17 18:30:19 INFO Tool registered name=get_today_date desc="Get today's date and time in RFC3339 format."
2025/11/17 18:30:19 INFO Tool registered name=get_weather_forecast desc="Provides a multi-day weather forecast (up to 7 days) for a given location."
--- PASS: TestTitle_EmptyConversation_Fallback (0.00s)
=== RUN   TestTitle_GeneratesConciseTitle_Integration
    title_test.go:29: skipping integration test: OPENAI_API_KEY not set
--- SKIP: TestTitle_GeneratesConciseTitle_Integration (0.00s)
PASS
ok      github.com/Neruzzz/acai-travel-challenge/internal/chat/assistant        0.278s
```

## Task 5



## References
- ChatGPT 5 for coding and syntax.
- WeatherAPI Documentation: https://www.weatherapi.com/docs/
- For deeper understanding of syntax and primitives in Go: https://go.dev/doc/
- Frankfurter API for exange rates tool: https://www.frankfurter.dev