package main

import (
	"context"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"healthmon/internal/api"
	"healthmon/internal/config"
	"healthmon/internal/db"
	"healthmon/internal/monitor"
	"healthmon/internal/store"
)

func main() {
	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if cfg.TelegramEnabled {
		if cfg.TelegramToken == "" || cfg.TelegramChatID == "" {
			log.Fatalf("telegram enabled but HM_TG_TOKEN or HM_TG_CHAT_ID missing")
		}
	}

	database, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer database.Close()

	if err := database.Migrate(ctx); err != nil {
		log.Fatalf("migrate db: %v", err)
	}

	st := store.New(database.SQL)
	if err := st.Load(ctx); err != nil {
		log.Fatalf("load store: %v", err)
	}

	broadcaster := api.NewBroadcaster()
	server := api.NewServer(st, broadcaster)
	if hasWebDist {
		server.WithStatic(http.FS(webDist))
	}
	mon := monitor.New(cfg, st, server)

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           server.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	serverErrCh := make(chan error, 1)
	go func() {
		serverErrCh <- httpServer.ListenAndServe()
	}()

	go func() {
		if err := mon.Start(ctx); err != nil && err != context.Canceled {
			log.Printf("monitor stopped: %v", err)
			stop()
		}
	}()

	log.Printf("healthmon starting on %s", cfg.HTTPAddr)
	var serverErr error
	select {
	case <-ctx.Done():
	case serverErr = <-serverErrCh:
		if serverErr != nil && serverErr != http.ErrServerClosed {
			log.Printf("http server stopped: %v", serverErr)
		}
		stop()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("http shutdown: %v", err)
	}
	if serverErr == nil {
		serverErr = <-serverErrCh
	}
	if serverErr != nil && serverErr != http.ErrServerClosed {
		log.Printf("http server stopped: %v", serverErr)
	}
}
