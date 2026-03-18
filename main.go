package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"home-automation-schedule-analytics-single-bin/internal/config"
	"home-automation-schedule-analytics-single-bin/internal/ingest"
	"home-automation-schedule-analytics-single-bin/internal/server"
	"home-automation-schedule-analytics-single-bin/internal/storage"
)

func main() {
	if err := run(); err != nil {
		log.Printf("fatal: %v", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
		return fmt.Errorf("create db dir: %w", err)
	}

	db, err := storage.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	if err := storage.InitSchema(context.Background(), db); err != nil {
		return fmt.Errorf("init schema: %w", err)
	}

	snapshotDir := filepath.Join(filepath.Dir(cfg.DBPath), "snapshots")
	if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
		return fmt.Errorf("create snapshot dir: %w", err)
	}

	staticFS := os.DirFS("static")

	handler := server.New(server.Config{
		DB: db,
		IngestCfg: ingest.Config{
			TimeZone:  cfg.TimeZone,
			Latitude:  cfg.Latitude,
			Longitude: cfg.Longitude,
		},
		SnapshotDir: snapshotDir,
		StaticFS:    staticFS,
	})

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      handler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		log.Printf("listening on :%s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		log.Printf("shutdown signal received")
	case err := <-errCh:
		return fmt.Errorf("server: %w", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}
