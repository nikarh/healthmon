package main

import (
	"context"
	"log"

	"healthmon/internal/config"
	"healthmon/internal/db"
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

	log.Printf("healthmon starting on %s", cfg.HTTPAddr)
}
