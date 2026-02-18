package main

import (
	"context"
	"log"
	"net/http"
	"os"
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

	database, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer database.Close()

	if err := database.Migrate(ctx); err != nil {
		log.Fatalf("migrate db: %v", err)
	}

	st := store.New(database.SQL, cfg.EventCacheLimit)
	if err := st.Load(ctx); err != nil {
		log.Fatalf("load store: %v", err)
	}

	broadcaster := api.NewBroadcaster()
	server := api.NewServer(st, broadcaster)
	mon := monitor.New(cfg, st, server)

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           server.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		if err := mon.Start(ctx); err != nil && err != context.Canceled {
			log.Printf("monitor stopped: %v", err)
			stop()
		}
	}()

	log.Printf("healthmon starting on %s", cfg.HTTPAddr)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("http server: %v", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)
	<-ctx.Done()
	os.Exit(0)
}
