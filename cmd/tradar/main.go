package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"tradar/internal/httpapi"
	"tradar/internal/storage"
)

func env(key, fallback string) string {
	if s := os.Getenv(key); s != "" {
		return s
	}
	return fallback
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
	cfg := httpapi.Config{WebDir: env("TRADAR_WEB_DIR", "web/dist"), AccessToken: token, DevOrigin: os.Getenv("TRADAR_DEV_ORIGIN"), AllowedHost: os.Getenv("TRADAR_ALLOWED_HOST")}
	server := &http.Server{Addr: addr, Handler: httpapi.New(db, cfg), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
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
