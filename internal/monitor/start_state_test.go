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
	"github.com/moby/moby/api/types/events"
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
			Image: "ghcr.io/example/imapsync:latest",
			Labels: map[string]string{
				"healthmon.role":             "service",
				"com.docker.compose.service": "imapsync",
			},
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

func TestStartReplaysMissedStartEventAfterSync(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()

	createdInspect := container.InspectResponse{
		ID:      "cid-qbt",
		Name:    "/qbittorrent",
		Created: now.Add(-time.Minute).Format(time.RFC3339Nano),
		State: &container.State{
			Status: "created",
		},
		Config: &container.Config{
			Image: "lscr.io/linuxserver/qbittorrent:latest",
			Labels: map[string]string{
				"healthmon.role":                         "service",
				"com.docker.compose.service":             "qbittorrent",
				"com.docker.compose.project":             "files",
				"com.docker.compose.project.working_dir": "/srv/files",
			},
		},
		HostConfig: &container.HostConfig{},
		Image:      "sha256:image-qbt",
	}
	runningInspect := createdInspect
	runningInspect.State = &container.State{
		Status:    "running",
		StartedAt: now.Add(-10 * time.Second).Format(time.RFC3339Nano),
	}

	createdRaw, err := json.Marshal(createdInspect)
	if err != nil {
		t.Fatalf("marshal created inspect: %v", err)
	}
	runningRaw, err := json.Marshal(runningInspect)
	if err != nil {
		t.Fatalf("marshal running inspect: %v", err)
	}

	mock := newMockDockerServer(t, []events.Message{
		{
			Type:     "container",
			Action:   "start",
			TimeNano: now.Add(-5 * time.Second).UnixNano(),
			Actor: events.Actor{
				ID: "cid-qbt",
				Attributes: map[string]string{
					"name": "qbittorrent",
				},
			},
		},
	}, []inspectRecord{
		{ID: "cid-qbt", Inspect: createdRaw},
		{ID: "cid-qbt", Inspect: runningRaw},
	})
	mock.requireSince = true
	mock.containers = []container.Summary{{ID: "cid-qbt"}}
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

	if _, err := st.AddEvent(ctx, store.Event{
		Container:   "qbittorrent",
		ContainerID: "old-cid",
		Type:        "stopped",
		Severity:    "blue",
		Message:     "Container stopped",
		Timestamp:   now.Add(-30 * time.Second),
	}); err != nil {
		t.Fatalf("seed latest event: %v", err)
	}

	srv := api.NewServer(st, api.NewBroadcaster(), api.WSOptions{})
	mon := New(config.Config{DockerHost: host}, st, srv)

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- mon.Start(runCtx)
	}()

	mock.AllowNext()

	deadline := time.After(2 * time.Second)
	for {
		got, ok := st.GetContainer("qbittorrent")
		if ok && got.Status == "running" && got.ContainerID == "cid-qbt" {
			break
		}
		select {
		case <-deadline:
			got, _ := st.GetContainer("qbittorrent")
			t.Fatalf("expected qbittorrent to be running after replayed start, got status=%q id=%q", got.Status, got.ContainerID)
		case <-time.After(20 * time.Millisecond):
		}
	}

	cancel()
	select {
	case err := <-done:
		if err != nil && err != context.Canceled {
			t.Fatalf("monitor error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("monitor did not exit")
	}
}
