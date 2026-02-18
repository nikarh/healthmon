package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"healthmon/internal/api"
	"healthmon/internal/config"
	"healthmon/internal/db"
	"healthmon/internal/store"
)

func main() {
	cfg := config.Load()
	ctx := context.Background()

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

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           server.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("healthmon starting on %s", cfg.HTTPAddr)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("http server: %v", err)
	}
}
