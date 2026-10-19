package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"tradar/internal/analysis"
	"tradar/internal/brief"
	"tradar/internal/httpapi"
	"tradar/internal/ingestion"
	"tradar/internal/storage"
)

func env(key, fallback string) string {
	if s := os.Getenv(key); s != "" {
		return s
	}
	return fallback
}

func modelRuntime(db *sql.DB) (*analysis.ModelRuntime, error) {
	providerName := strings.ToLower(strings.TrimSpace(os.Getenv("TRADAR_MODEL_PROVIDER")))
	if providerName == "" {
		return analysis.NewModelRuntime(db, nil, analysis.ModelConfig{}), nil
	}
	if providerName != "openai" {
		return nil, fmt.Errorf("TRADAR_MODEL_PROVIDER only supports openai")
	}
	modelName := strings.TrimSpace(os.Getenv("TRADAR_MODEL_NAME"))
	apiKey := os.Getenv("TRADAR_OPENAI_API_KEY")
	maxOutput, err := strconv.Atoi(os.Getenv("TRADAR_MODEL_MAX_OUTPUT_TOKENS"))
	if err != nil {
		return nil, fmt.Errorf("TRADAR_MODEL_MAX_OUTPUT_TOKENS must be an integer")
	}
	dailyBudget, err := strconv.Atoi(os.Getenv("TRADAR_MODEL_DAILY_TOKEN_BUDGET"))
	if err != nil {
		return nil, fmt.Errorf("TRADAR_MODEL_DAILY_TOKEN_BUDGET must be an integer")
	}
	timeout, err := time.ParseDuration(env("TRADAR_MODEL_TIMEOUT", "45s"))
	if err != nil {
		return nil, fmt.Errorf("TRADAR_MODEL_TIMEOUT must be a duration")
	}
	provider, err := analysis.NewOpenAIProvider(apiKey)
	if err != nil {
		return nil, err
	}
	config := analysis.ModelConfig{
		Provider: providerName, Model: modelName, PromptVersion: analysis.ModelPromptVersion,
		MaxOutputTokens: maxOutput, DailyTokenBudget: dailyBudget, Timeout: timeout,
	}
	runtime := analysis.NewModelRuntime(db, provider, config)
	if !runtime.Enabled() {
		return nil, fmt.Errorf("model configuration is incomplete or outside supported limits")
	}
	return runtime, nil
}
func run() error {
	addr := env("TRADAR_ADDR", "127.0.0.1:8080")
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		return fmt.Errorf("TRADAR_ADDR must be host:port")
	}
	ip := net.ParseIP(host)
	token := os.Getenv("TRADAR_ACCESS_TOKEN")
	local := strings.EqualFold(host, "localhost") || (ip != nil && ip.IsLoopback())
	if !local && len(token) < 32 {
		return fmt.Errorf("non-loopback binding requires TRADAR_ACCESS_TOKEN of at least 32 characters; prefer an SSH tunnel")
	}
	db, err := storage.Open(env("TRADAR_DB_PATH", "data/radar.db"))
	if err != nil {
		return err
	}
	defer db.Close()
	analysisRuntime, err := modelRuntime(db)
	if err != nil {
		return err
	}
	if interrupted, recoverErr := ingestion.RecoverInterrupted(context.Background(), db, time.Now()); recoverErr != nil {
		return fmt.Errorf("recover ingestion tasks: %w", recoverErr)
	} else if interrupted > 0 {
		slog.Warn("interrupted unfinished ingestion tasks", "count", interrupted)
	}
	if interrupted, recoverErr := brief.RecoverInterrupted(context.Background(), db, time.Now()); recoverErr != nil {
		return fmt.Errorf("recover brief tasks: %w", recoverErr)
	} else if interrupted > 0 {
		slog.Warn("interrupted unfinished brief tasks", "count", interrupted)
	}
	if interrupted, recoverErr := analysis.RecoverModelInterrupted(context.Background(), db, time.Now()); recoverErr != nil {
		return fmt.Errorf("recover model analysis tasks: %w", recoverErr)
	} else if interrupted > 0 {
		slog.Warn("interrupted unfinished model analysis tasks", "count", interrupted)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	runtimeContext, stopRuntime := context.WithCancel(ctx)
	githubToken := os.Getenv("TRADAR_GITHUB_TOKEN")
	productHuntToken := os.Getenv("TRADAR_PRODUCT_HUNT_TOKEN")
	ingestionRuntime := ingestion.NewRuntime(db, ingestion.RuntimeConfig{
		GitHubToken: githubToken, ProductHuntToken: productHuntToken,
	})
	ingestionRuntime.Start(runtimeContext)
	briefRuntime := brief.NewRuntime(db, brief.RuntimeConfig{})
	briefRuntime.Start(runtimeContext)
	analysisRuntime.Start(runtimeContext)
	defer func() {
		stopRuntime()
		ingestionRuntime.Wait()
		briefRuntime.Wait()
		analysisRuntime.Wait()
	}()
	cfg := httpapi.Config{
		WebDir: env("TRADAR_WEB_DIR", "web/dist"), AccessToken: token,
		DevOrigin: os.Getenv("TRADAR_DEV_ORIGIN"), AllowedHost: os.Getenv("TRADAR_ALLOWED_HOST"),
		HasGitHubCredentials:      githubToken != "",
		HasProductHuntCredentials: productHuntToken != "",
		QueueIngestionRun:         ingestionRuntime.Enqueue,
		QueueBriefRun:             briefRuntime.Enqueue,
		AnalysisModel:             analysisRuntime,
	}
	server := &http.Server{Addr: addr, Handler: httpapi.New(db, cfg), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	result := make(chan error, 1)
	go func() { slog.Info("TRadar listening", "address", addr); result <- server.ListenAndServe() }()
	select {
	case err := <-result:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			return err
		}
	}
	return nil
}
func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	if err := run(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
