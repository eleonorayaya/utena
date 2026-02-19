package api

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/workspace"
	"github.com/eleonorayaya/utena/internal/zellij"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/httplog/v3"
	"github.com/go-chi/render"
	"github.com/spf13/afero"
)

func StartDaemon() {
	prettyLogs := os.Getenv("UTENA_LOG_PRETTY") == "true"

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	bus := eventbus.NewEventBus()

	homeDir, _ := os.UserHomeDir()
	configDir := filepath.Join(homeDir, ".config", "utena")

	workspaceModule := workspace.NewWorkspaceModule(afero.NewOsFs(), configDir)
	sessionModule := session.NewSessionModule(workspaceModule, bus, afero.NewOsFs(), configDir)
	zellijModule := zellij.NewZellijModule(sessionModule, bus)
	claudeModule := claude.NewClaudeModule(afero.NewOsFs(), configDir)

	if err := workspaceModule.OnAppStart(ctx); err != nil {
		log.Fatalf("Failed to initialize workspace module: %v", err)
	}

	if err := sessionModule.OnAppStart(ctx); err != nil {
		log.Fatalf("Failed to initialize session module: %v", err)
	}

	if err := zellijModule.OnAppStart(ctx); err != nil {
		log.Fatalf("Failed to initialize zellij module: %v", err)
	}

	if err := claudeModule.OnAppStart(ctx); err != nil {
		log.Fatalf("Failed to initialize claude module: %v", err)
	}

	go serveAPI(ctx, workspaceModule, sessionModule, zellijModule, claudeModule, prettyLogs)

	<-ctx.Done()

	if err := claudeModule.OnAppEnd(ctx); err != nil {
		log.Printf("Error cleaning up claude module: %v", err)
	}

	if err := zellijModule.OnAppEnd(ctx); err != nil {
		log.Printf("Error cleaning up zellij module: %v", err)
	}

	if err := sessionModule.OnAppEnd(ctx); err != nil {
		log.Printf("Error cleaning up session module: %v", err)
	}

	if err := workspaceModule.OnAppEnd(ctx); err != nil {
		log.Printf("Error cleaning up workspace module: %v", err)
	}
}

func serveAPI(ctx context.Context, workspaceModule *workspace.WorkspaceModule, sessionModule *session.SessionModule, zellijModule *zellij.ZellijModule, claudeModule *claude.ClaudeModule, prettyLogs bool) {
	r := chi.NewRouter()

	httplogOpts := &httplog.Options{}
	if prettyLogs {
		httplogOpts.Schema = httplog.SchemaECS.Concise(true)
	}

	r.Use(httplog.RequestLogger(slog.Default(), httplogOpts))
	r.Use(middleware.Recoverer)
	r.Use(middleware.URLFormat)
	r.Use(render.SetContentType(render.ContentTypeJSON))

	r.Mount("/workspaces", workspaceModule.Routes())
	r.Mount("/sessions", sessionModule.Routes())
	r.Mount("/zellij", zellijModule.Routes())
	r.Mount("/claude", claudeModule.Routes())

	log.Println("Starting daemon on :3333")
	http.ListenAndServe(":3333", r)
}
