package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"

	"github.com/Neruzzz/acai-travel-challenge/internal/chat/model"
	"github.com/Neruzzz/acai-travel-challenge/internal/tools"

	"github.com/openai/openai-go/v2"
)

type Assistant struct {
	cli openai.Client
}

func New() *Assistant {
	a := &Assistant{cli: openai.NewClient()}

	ts := tools.AllTools()
	if len(ts) == 0 {
		slog.Warn("No tools registered! Check package names, init() and build tags.")
	} else {
		slog.Info("Tools registered", "count", len(ts))
		for _, t := range ts {
			slog.Info("Tool registered", "name", t.Name(), "desc", t.Description())
		}
	}

	return a
}

func (a *Assistant) Title(ctx context.Context, conv *model.Conversation) (string, error) {
	if len(conv.Messages) == 0 {
		return "An empty conversation", nil
	}
	slog.InfoContext(ctx, "Generating title for conversation", "conversation_id", conv.ID)

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

	system := openai.SystemMessage(`You generate concise conversation titles.

	Rules:
	- Output ONLY a short noun phrase summarizing the user's first message.
	- Do NOT answer the question.
	- Do NOT include quotes.
	- Maximum 6 words.`)

	user := openai.UserMessage(firstUserMessage)

	resp, err := a.cli.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:    openai.ChatModelGPT4_1,
		Messages: []openai.ChatCompletionMessageParamUnion{system, user},
	})
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
	return title, nil
}

func (a *Assistant) Reply(ctx context.Context, conv *model.Conversation) (string, error) {
	if len(conv.Messages) == 0 {
		return "", errors.New("conversation has no messages")
	}
	slog.InfoContext(ctx, "Generating reply for conversation", "conversation_id", conv.ID)

	msgs := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage("You are a helpful, concise AI assistant. Provide accurate, safe, and clear responses."),
	}
	for _, m := range conv.Messages {
		switch m.Role {
		case model.RoleUser:
			msgs = append(msgs, openai.UserMessage(m.Content))
		case model.RoleAssistant:
			msgs = append(msgs, openai.AssistantMessage(m.Content))
		}
	}

	// Dynamic tool exposure
	var toolDefs []openai.ChatCompletionToolUnionParam
	for _, t := range tools.AllTools() {
		toolDefs = append(toolDefs,
			openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
				Name:        t.Name(),
				Description: openai.String(t.Description()),
				Parameters:  t.ParametersSchema(),
			}),
		)
	}

	for i := 0; i < 15; i++ {
		resp, err := a.cli.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
			Model:    openai.ChatModelGPT4_1,
			Messages: msgs,
			Tools:    toolDefs,
		})
		if err != nil {
			return "", err
		}
		if len(resp.Choices) == 0 {
			return "", errors.New("no choices returned by OpenAI")
		}

		message := resp.Choices[0].Message
		if len(message.ToolCalls) == 0 {
			return message.Content, nil
		}

		msgs = append(msgs, message.ToParam())

		for _, call := range message.ToolCalls {
			slog.InfoContext(ctx, "Tool call received", "name", call.Function.Name, "args", call.Function.Arguments)

			t := tools.FindByName(call.Function.Name)
			if t == nil {
				msgs = append(msgs, openai.ToolMessage("unknown tool: "+call.Function.Name, call.ID))
				continue
			}

			var args map[string]any
			if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
				msgs = append(msgs, openai.ToolMessage("failed to parse tool arguments: "+err.Error(), call.ID))
				continue
			}

			out, err := t.Call(ctx, args)
			if err != nil {
				msgs = append(msgs, openai.ToolMessage("tool error: "+err.Error(), call.ID))
				continue
			}

			msgs = append(msgs, openai.ToolMessage(out, call.ID))
		}
	}

	return "", errors.New("too many tool calls, unable to generate reply")
}
