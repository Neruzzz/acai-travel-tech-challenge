package chat

import (
	"context"
	"testing"

	"github.com/Neruzzz/acai-travel-challenge/internal/chat/model"
	. "github.com/Neruzzz/acai-travel-challenge/internal/chat/testing"
	"github.com/Neruzzz/acai-travel-challenge/internal/pb"
	"github.com/google/go-cmp/cmp"
	"github.com/twitchtv/twirp"
	"google.golang.org/protobuf/testing/protocmp"
)

type fakeAssistant struct {
	title string
	reply string
}

func (f fakeAssistant) Title(_ context.Context, _ *model.Conversation) (string, error) {
	return f.title, nil
}

func (f fakeAssistant) Reply(_ context.Context, _ *model.Conversation) (string, error) {
	return f.reply, nil
}

func TestServer_StartConversation_Creates_Populates_Triggers(t *testing.T) {
	ctx := context.Background()

	const wantTitle = "Weather in Barcelona"
	const wantReply = "Right now it’s 18°C with light rain."

	srv := NewServer(model.New(ConnectMongo()), fakeAssistant{
		title: wantTitle,
		reply: wantReply,
	})

	t.Run("creates conversation, sets title, triggers assistant reply",
		WithFixture(func(t *testing.T, _ *Fixture) {
			req := &pb.StartConversationRequest{
				Message: "What is the weather like in Barcelona?",
			}

			res, err := srv.StartConversation(ctx, req)
			if err != nil {
				t.Fatalf("StartConversation() unexpected error: %v", err)
			}

			if res.GetConversationId() == "" {
				t.Error("expected non-empty ConversationId")
			}
			if res.GetTitle() != wantTitle {
				t.Errorf("title mismatch: got %q, want %q", res.GetTitle(), wantTitle)
			}
			if res.GetReply() != wantReply {
				t.Errorf("reply mismatch: got %q, want %q", res.GetReply(), wantReply)
			}

			out, err := srv.DescribeConversation(ctx, &pb.DescribeConversationRequest{
				ConversationId: res.GetConversationId(),
			})
			if err != nil {
				t.Fatalf("DescribeConversation() error: %v", err)
			}

			conv := out.GetConversation()
			if conv == nil {
				t.Fatal("DescribeConversation() returned nil conversation")
			}
			if conv.GetTitle() != wantTitle {
				t.Errorf("persisted title mismatch: got %q, want %q", conv.GetTitle(), wantTitle)
			}

			msgs := conv.GetMessages()
			if len(msgs) < 2 {
				t.Fatalf("expected at least 2 messages (user + assistant), got %d", len(msgs))
			}
			if got := msgs[0].GetRole(); got != pb.Conversation_USER {
				t.Errorf("first message role = %v, want %v", got, pb.Conversation_USER)
			}
			if got := msgs[1].GetRole(); got != pb.Conversation_ASSISTANT {
				t.Errorf("second message role = %v, want %v", got, pb.Conversation_ASSISTANT)
			}
			if got := msgs[1].GetContent(); got != wantReply {
				t.Errorf("assistant content mismatch: got %q, want %q", got, wantReply)
			}
		}))
}

func TestServer_StartConversation_EmptyMessage_Err(t *testing.T) {
	ctx := context.Background()
	srv := NewServer(model.New(ConnectMongo()), fakeAssistant{
		title: "ignored",
		reply: "ignored",
	})

	t.Run("empty message should return InvalidArgument",
		WithFixture(func(t *testing.T, _ *Fixture) {
			_, err := srv.StartConversation(ctx, &pb.StartConversationRequest{Message: ""})
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			te, ok := err.(twirp.Error)
			if !ok {
				t.Fatalf("expected twirp.Error, got %T", err)
			}
			if te.Code() != twirp.InvalidArgument {
				t.Fatalf("expected twirp.InvalidArgument, got %v", te.Code())
			}
		}))
}

func TestServer_DescribeConversation(t *testing.T) {
	ctx := context.Background()
	srv := NewServer(model.New(ConnectMongo()), nil)

	t.Run("describe existing conversation", WithFixture(func(t *testing.T, f *Fixture) {
		c := f.CreateConversation()

		out, err := srv.DescribeConversation(ctx, &pb.DescribeConversationRequest{ConversationId: c.ID.Hex()})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		got, want := out.GetConversation(), c.Proto()
		if !cmp.Equal(got, want, protocmp.Transform()) {
			t.Errorf("DescribeConversation() mismatch (-got +want):\n%s", cmp.Diff(got, want, protocmp.Transform()))
		}
	}))

	t.Run("describe non existing conversation should return 404", WithFixture(func(t *testing.T, f *Fixture) {
		_, err := srv.DescribeConversation(ctx, &pb.DescribeConversationRequest{ConversationId: "08a59244257c872c5943e2a2"})
		if err == nil {
			t.Fatal("expected error for non-existing conversation, got nil")
		}

		if te, ok := err.(twirp.Error); !ok || te.Code() != twirp.NotFound {
			t.Fatalf("expected twirp.NotFound error, got %v", err)
		}
	}))
}
