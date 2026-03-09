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

func TestHistoryKeepsParsedContainerNamesSeparateFromServiceName(t *testing.T) {
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
		Name:                 "affine",
		ContainerID:          "cid-rename",
		CurrentContainerName: "elastic_ride",
		Image:                "affine",
		ImageTag:             "stable",
		ImageID:              "img-affine",
		CreatedAt:            now.Add(-time.Hour),
		RegisteredAt:         now.Add(-time.Hour),
		StartedAt:            now.Add(-30 * time.Minute),
		Status:               "running",
		Role:                 "service",
		Caps:                 []string{},
		ReadOnly:             true,
		User:                 "1000:1000",
		UpdatedAt:            now,
		Present:              true,
		RestartLoop:          true,
		RestartStreak:        7,
	}
	if err := st.UpsertContainer(ctx, cont); err != nil {
		t.Fatalf("upsert container: %v", err)
	}
	created, ok := st.GetContainer("affine")
	if !ok {
		t.Fatalf("expected container in cache")
	}

	if _, err := st.AddEvent(ctx, Event{
		ContainerPK:         created.ID,
		Container:           created.Name,
		ContainerID:         created.ContainerID,
		ParsedContainerName: "elastic_ride",
		Type:                "restart",
		Severity:            "blue",
		Message:             "Restart event: die",
		Timestamp:           now.Add(time.Second),
		Reason:              "die",
	}); err != nil {
		t.Fatalf("add event: %v", err)
	}
	if _, err := st.AddAlert(ctx, Alert{
		ContainerPK:         created.ID,
		Container:           created.Name,
		ContainerID:         created.ContainerID,
		ParsedContainerName: "elastic_ride",
		Type:                "restart_loop",
		Severity:            "red",
		Message:             "Restart loop detected",
		Timestamp:           now.Add(2 * time.Second),
	}); err != nil {
		t.Fatalf("add alert: %v", err)
	}

	renamed := created
	renamed.CurrentContainerName = "affine"
	renamed.UpdatedAt = now.Add(3 * time.Second)
	if err := st.RenameContainer(ctx, "elastic_ride", "affine", renamed); err != nil {
		t.Fatalf("rename container: %v", err)
	}

	events, err := st.ListAllEvents(ctx, 0, 10)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Container != "affine" {
		t.Fatalf("expected service name affine, got %q", events[0].Container)
	}
	if events[0].ParsedContainerName != "elastic_ride" {
		t.Fatalf("expected parsed event name elastic_ride, got %q", events[0].ParsedContainerName)
	}

	alerts, err := st.ListAllAlerts(ctx, 0, 10)
	if err != nil {
		t.Fatalf("list alerts: %v", err)
	}
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}
	if alerts[0].Container != "affine" {
		t.Fatalf("expected service name affine, got %q", alerts[0].Container)
	}
	if alerts[0].ParsedContainerName != "elastic_ride" {
		t.Fatalf("expected parsed alert name elastic_ride, got %q", alerts[0].ParsedContainerName)
	}

	updated, ok := st.GetContainer("affine")
	if !ok {
		t.Fatalf("expected renamed service in cache")
	}
	if updated.CurrentContainerName != "affine" {
		t.Fatalf("expected current container name affine, got %q", updated.CurrentContainerName)
	}
}

func TestUpsertMergesContainerIDDuplicateIntoCanonicalService(t *testing.T) {
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
	placeholder := Container{
		Name:                 "729d4232bd38_imapsync",
		ContainerID:          "cid-imapsync",
		CurrentContainerName: "729d4232bd38_imapsync",
		Image:                "docker.io/nikarh/fileserver-imapsync",
		ImageTag:             "latest",
		ImageID:              "sha256:imapsync",
		CreatedAt:            now.Add(-time.Hour),
		RegisteredAt:         now.Add(-time.Hour),
		StartedAt:            now.Add(-time.Hour),
		Status:               "created",
		Role:                 "service",
		Caps:                 []string{},
		User:                 "0:0",
		UpdatedAt:            now.Add(-time.Minute),
		Present:              true,
	}
	if err := st.UpsertContainer(ctx, placeholder); err != nil {
		t.Fatalf("upsert placeholder: %v", err)
	}
	duplicate, ok := st.GetContainer("729d4232bd38_imapsync")
	if !ok {
		t.Fatalf("expected duplicate placeholder in cache")
	}

	canonical := Container{
		Name:                 "imapsync",
		ContainerID:          "cid-imapsync",
		CurrentContainerName: "imapsync",
		Image:                "docker.io/nikarh/fileserver-imapsync",
		ImageTag:             "latest",
		ImageID:              "sha256:imapsync",
		CreatedAt:            placeholder.CreatedAt,
		RegisteredAt:         placeholder.RegisteredAt,
		StartedAt:            placeholder.StartedAt,
		Status:               "running",
		Role:                 "service",
		Caps:                 []string{},
		User:                 "0:0",
		UpdatedAt:            now,
		Present:              true,
	}
	if err := st.UpsertContainer(ctx, canonical); err != nil {
		t.Fatalf("upsert canonical: %v", err)
	}

	merged, ok := st.GetContainer("imapsync")
	if !ok {
		t.Fatalf("expected canonical service in cache")
	}
	if merged.ID != duplicate.ID {
		t.Fatalf("expected canonical service to reuse duplicate row id %d, got %d", duplicate.ID, merged.ID)
	}
	if merged.CurrentContainerName != "imapsync" {
		t.Fatalf("expected runtime name to be updated, got %q", merged.CurrentContainerName)
	}
	if _, ok := st.GetContainer("729d4232bd38_imapsync"); ok {
		t.Fatalf("expected duplicate runtime-name row to be removed from cache")
	}

	var count int
	if err := dbConn.SQL.QueryRowContext(ctx, `SELECT COUNT(1) FROM containers WHERE container_id = ?`, canonical.ContainerID).Scan(&count); err != nil {
		t.Fatalf("count merged rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one row for container id, got %d", count)
	}
}
