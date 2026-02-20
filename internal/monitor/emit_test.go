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

func TestEmitPrefersContainerID(t *testing.T) {
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
	victoria := store.Container{
		Name:         "victoria-logs",
		ContainerID:  "aaa111",
		Image:        "victoria",
		ImageTag:     "latest",
		ImageID:      "img-victoria",
		CreatedAt:    now.Add(-time.Hour),
		RegisteredAt: now.Add(-time.Hour),
		StartedAt:    now.Add(-30 * time.Minute),
		Status:       "running",
		Role:         "service",
		Caps:         []string{},
		ReadOnly:     false,
		User:         "0:0",
		UpdatedAt:    now.Add(-time.Hour),
		Present:      true,
	}
	imapsync := store.Container{
		Name:         "imapsync",
		ContainerID:  "bbb222",
		Image:        "imapsync",
		ImageTag:     "latest",
		ImageID:      "img-imapsync",
		CreatedAt:    now.Add(-time.Hour),
		RegisteredAt: now.Add(-time.Hour),
		StartedAt:    now.Add(-30 * time.Minute),
		Status:       "running",
		Role:         "service",
		Caps:         []string{},
		ReadOnly:     false,
		User:         "0:0",
		UpdatedAt:    now.Add(-time.Hour),
		Present:      true,
	}

	if err := st.UpsertContainer(ctx, victoria); err != nil {
		t.Fatalf("upsert victoria: %v", err)
	}
	if err := st.UpsertContainer(ctx, imapsync); err != nil {
		t.Fatalf("upsert imapsync: %v", err)
	}

	server := api.NewServer(st, api.NewBroadcaster(), api.WSOptions{})
	mon := New(config.Config{}, st, server)

	mon.emit(ctx, store.Event{
		Container:   "imapsync",
		ContainerID: "aaa111",
		Type:        "started",
		Severity:    "blue",
		Message:     "Container started",
		Timestamp:   now,
		Reason:      "start",
	})

	events, err := st.ListAllEvents(ctx, 0, 10)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) == 0 {
		t.Fatalf("expected event to be recorded")
	}
	got := events[0]
	if got.Container != "victoria-logs" {
		t.Fatalf("expected container victoria-logs, got %q", got.Container)
	}
	if got.ContainerID != "aaa111" {
		t.Fatalf("expected container id aaa111, got %q", got.ContainerID)
	}
}
