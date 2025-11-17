package assistant_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/Neruzzz/acai-travel-challenge/internal/chat/assistant"
	"github.com/Neruzzz/acai-travel-challenge/internal/chat/model"
)

func TestTitle_EmptyConversation_Fallback(t *testing.T) {
	ctx := context.Background()
	a := assistant.New()

	conv := &model.Conversation{Messages: nil}
	title, err := a.Title(ctx, conv)
	if err != nil {
		t.Fatalf("Title() unexpected error: %v", err)
	}
	if title != "An empty conversation" {
		t.Errorf("Title() fallback mismatch: got %q, want %q", title, "An empty conversation")
	}
}

func TestTitle_GeneratesConciseTitle_Integration(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("skipping integration test: OPENAI_API_KEY not set")
	}
	ctx := context.Background()
	a := assistant.New()

	conv := &model.Conversation{
		Messages: []*model.Message{
			{Role: model.RoleUser, Content: "What is the weather like in Barcelona?"},
		},
	}

	title, err := a.Title(ctx, conv)
	if err != nil {
		t.Fatalf("Title() error: %v", err)
	}
	if strings.TrimSpace(title) == "" {
		t.Fatal("Title() returned empty")
	}
	if len(title) > 80 {
		t.Errorf("Title() too long: %d chars", len(title))
	}
	if strings.ContainsAny(title, "\"'\n") {
		t.Errorf("Title() should not contain quotes/newlines: %q", title)
	}
	if strings.HasSuffix(title, "?") {
		t.Errorf("Title() should be a noun phrase, not a question: %q", title)
	}
}
