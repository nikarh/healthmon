package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"healthmon/internal/db"
)

func TestUpsertKeepsContainerIDAfterEvents(t *testing.T) {
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

	st := New(dbConn.SQL)
	if err := st.Load(ctx); err != nil {
		t.Fatalf("load store: %v", err)
	}

	now := time.Now().UTC()
	cont := Container{
		Name:         "imapsync",
		ContainerID:  "container-aaa",
		Image:        "imapsync",
		ImageTag:     "latest",
		ImageID:      "img-imapsync",
		CreatedAt:    now,
		RegisteredAt: now,
		StartedAt:    now,
		Status:       "running",
		Role:         "service",
		Caps:         []string{},
		ReadOnly:     false,
		User:         "0:0",
		UpdatedAt:    now,
		Present:      true,
	}

	if err := st.UpsertContainer(ctx, cont); err != nil {
		t.Fatalf("upsert container: %v", err)
	}
	created, ok := st.GetContainer("imapsync")
	if !ok {
		t.Fatalf("expected container in cache")
	}
	if created.ID == 0 {
		t.Fatalf("expected container id to be set")
	}

	// Add events to advance sqlite last_insert_rowid.
	_, err = st.AddEvent(ctx, Event{
		ContainerPK: created.ID,
		Container:   created.Name,
		ContainerID: created.ContainerID,
		Type:        "restart",
		Severity:    "blue",
		Message:     "Restart event: die",
		Timestamp:   now.Add(time.Second),
		Reason:      "die",
		DetailsJSON: string(mustJSON(map[string]string{"test": "1"})),
	})
	if err != nil {
		t.Fatalf("add event 1: %v", err)
	}
	_, err = st.AddEvent(ctx, Event{
		ContainerPK: created.ID,
		Container:   created.Name,
		ContainerID: created.ContainerID,
		Type:        "started",
		Severity:    "blue",
		Message:     "Container started",
		Timestamp:   now.Add(2 * time.Second),
		Reason:      "start",
		DetailsJSON: string(mustJSON(map[string]string{"test": "2"})),
	})
	if err != nil {
		t.Fatalf("add event 2: %v", err)
	}

	cont.ImageTag = "v2"
	if err := st.UpsertContainer(ctx, cont); err != nil {
		t.Fatalf("upsert container 2: %v", err)
	}

	updated, ok := st.GetContainer("imapsync")
	if !ok {
		t.Fatalf("expected container after update")
	}
	if updated.ID != created.ID {
		t.Fatalf("expected container id to remain %d, got %d", created.ID, updated.ID)
	}
}
