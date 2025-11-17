package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Neruzzz/acai-travel-challenge/internal/chat"
	"github.com/Neruzzz/acai-travel-challenge/internal/chat/assistant"
	"github.com/Neruzzz/acai-travel-challenge/internal/chat/model"
	"github.com/Neruzzz/acai-travel-challenge/internal/httpx"
	"github.com/Neruzzz/acai-travel-challenge/internal/mongox"
	"github.com/Neruzzz/acai-travel-challenge/internal/pb"
	"github.com/gorilla/mux"
	"github.com/twitchtv/twirp"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func main() {
	ctx := context.Background()

	shutdown, err := httpx.InitTelemetry(ctx, "acai-server")
	if err != nil {
		log.Fatalf("telemetry init error: %v", err)
	}
	defer func() { _ = shutdown(context.Background()) }()

	mongo := mongox.MustConnect()
	repo := model.New(mongo)
	assist := assistant.New()
	server := chat.NewServer(repo, assist)

	r := mux.NewRouter()
	r.Use(
		httpx.Logger(),
		httpx.Recovery(),
	)

	r.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, "Hi, my name is Clippy!")
	})

	twirpHandler := pb.NewChatServiceServer(server, twirp.WithServerJSONSkipDefaults(true))
	instrumentedTwirp := otelhttp.NewHandler(
		httpx.MetricsMiddleware(twirpHandler),
		"twirp.chatservice",
	)
	r.PathPrefix("/twirp/").Handler(instrumentedTwirp)

	httpServer := &http.Server{
		Addr:    ":8080",
		Handler: r,
	}

	slog.Info("Starting the server...")
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http server error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(ctx)
}
