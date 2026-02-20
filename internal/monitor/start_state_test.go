package monitor

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"healthmon/internal/api"
	"healthmon/internal/config"
	"healthmon/internal/db"
	"healthmon/internal/store"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

func TestHandleStartPreservesRestartLoopForAutoRestartContainers(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()

	inspect := container.InspectResponse{
		ID:      "cid-auto",
		Created: now.Add(-time.Hour).Format(time.RFC3339Nano),
		State: &container.State{
			Status:    "running",
			StartedAt: now.Add(-2 * time.Minute).Format(time.RFC3339Nano),
		},
		HostConfig: &container.HostConfig{
			RestartPolicy: container.RestartPolicy{Name: "always"},
		},
		Config: &container.Config{
			Image:  "ghcr.io/example/imapsync:latest",
			Labels: map[string]string{"healthmon.role": "service"},
		},
		Image: "sha256:image-auto",
	}
	raw, err := json.Marshal(inspect)
	if err != nil {
		t.Fatalf("marshal inspect: %v", err)
	}

	mock := newMockDockerServer(t, nil, []inspectRecord{
		{ID: "cid-auto", Inspect: raw},
	})
	host, err := mock.Start()
	if err != nil {
		t.Fatalf("start mock docker: %v", err)
	}
	defer mock.Close()

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

	existing := store.Container{
		Name:          "imapsync",
		ContainerID:   "cid-auto",
		Image:         "ghcr.io/example/imapsync",
		ImageTag:      "latest",
		ImageID:       "sha256:image-prev",
		CreatedAt:     now.Add(-3 * time.Hour),
		RegisteredAt:  now.Add(-3 * time.Hour),
		StartedAt:     now.Add(-time.Hour),
		Status:        "running",
		Role:          "service",
		Caps:          []string{},
		User:          "0:0",
		Present:       true,
		RestartLoop:   true,
		RestartStreak: 7,
		UpdatedAt:     now.Add(-time.Minute),
	}
	if err := st.UpsertContainer(ctx, existing); err != nil {
		t.Fatalf("upsert existing: %v", err)
	}

	srv := api.NewServer(st, api.NewBroadcaster(), api.WSOptions{})
	mon := New(config.Config{}, st, srv)
	cli, err := client.NewClientWithOpts(client.WithHost(host), client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("new docker client: %v", err)
	}
	mon.docker = cli

	mon.handleStart(ctx, "imapsync", "cid-auto")

	got, ok := st.GetContainer("imapsync")
	if !ok {
		t.Fatalf("container not found")
	}
	if !got.RestartLoop {
		t.Fatalf("expected restart_loop to stay true")
	}
	if got.RestartStreak != 7 {
		t.Fatalf("expected restart_streak=7, got %d", got.RestartStreak)
	}
}
