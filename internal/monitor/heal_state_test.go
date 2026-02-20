package monitor

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"healthmon/internal/api"
	"healthmon/internal/config"
	"healthmon/internal/db"
	"healthmon/internal/store"
)

func TestCheckHealsClearsPersistedRestartLoopWithoutInMemoryTracker(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "healthmon.db")
	dbConn, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer dbConn.Close()

	if err := dbConn.Migrate(ctx); err != nil {
		t.Fatalf("migrate db: %v", err)
	}

	st := store.New(dbConn.SQL)
	if err := st.Load(ctx); err != nil {
		t.Fatalf("load store: %v", err)
	}

	now := time.Now().UTC()
	c := store.Container{
		Name:          "imapsync",
		ContainerID:   "cid-1",
		Image:         "ghcr.io/example/imapsync",
		ImageTag:      "latest",
		ImageID:       "sha256:image",
		CreatedAt:     now.Add(-3 * time.Hour),
		RegisteredAt:  now.Add(-3 * time.Hour),
		StartedAt:     now.Add(-20 * time.Minute),
		Status:        "running",
		Role:          "service",
		Caps:          []string{},
		ReadOnly:      false,
		User:          "0:0",
		Present:       true,
		RestartLoop:   true,
		RestartStreak: 6,
		UpdatedAt:     now.Add(-20 * time.Minute),
	}
	if err := st.UpsertContainer(ctx, c); err != nil {
		t.Fatalf("upsert container: %v", err)
	}
	container, ok := st.GetContainer("imapsync")
	if !ok {
		t.Fatalf("container missing")
	}
	if _, err := st.AddEvent(ctx, store.Event{
		ContainerPK: container.ID,
		Container:   container.Name,
		ContainerID: container.ContainerID,
		Type:        "restart",
		Severity:    "blue",
		Message:     "Restart event: die",
		Timestamp:   now.Add(-2 * time.Minute),
		Reason:      "die",
	}); err != nil {
		t.Fatalf("add restart event: %v", err)
	}

	server := api.NewServer(st, api.NewBroadcaster(), api.WSOptions{})
	mon := New(config.Config{
		RestartWindowSeconds: 30,
		RestartThreshold:     3,
	}, st, server)

	mon.checkHeals(ctx)

	updated, ok := st.GetContainer("imapsync")
	if !ok {
		t.Fatalf("updated container missing")
	}
	if updated.RestartLoop {
		t.Fatalf("expected restart_loop to be cleared")
	}
	if updated.RestartStreak != 0 {
		t.Fatalf("expected restart_streak=0, got %d", updated.RestartStreak)
	}

	alerts, err := st.ListAllAlerts(ctx, 0, 20)
	if err != nil {
		t.Fatalf("list alerts: %v", err)
	}
	found := false
	for _, a := range alerts {
		if a.Type == "restart_healed" && a.Container == "imapsync" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected restart_healed alert")
	}
}
