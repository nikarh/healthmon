package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"healthmon/internal/db"
)

func TestLoadRepairsEventAssociations(t *testing.T) {
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

	now := time.Now().UTC().Format(time.RFC3339)
	caps, _ := json.Marshal([]string{})

	// Insert two containers.
	res, err := dbConn.SQL.ExecContext(ctx, `
INSERT INTO containers (name, container_id, image, image_tag, image_id, created_at_container, first_seen_at, status, role, caps, read_only, user, updated_at, present)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"victoria-logs",
		"aaa111",
		"victoria",
		"latest",
		"img-victoria",
		now,
		now,
		"running",
		"service",
		string(caps),
		0,
		"0:0",
		now,
		1,
	)
	if err != nil {
		t.Fatalf("insert victoria: %v", err)
	}
	victoriaID, _ := res.LastInsertId()

	res, err = dbConn.SQL.ExecContext(ctx, `
INSERT INTO containers (name, container_id, image, image_tag, image_id, created_at_container, first_seen_at, status, role, caps, read_only, user, updated_at, present)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"imapsync",
		"bbb222",
		"imapsync",
		"latest",
		"img-imapsync",
		now,
		now,
		"running",
		"service",
		string(caps),
		0,
		"0:0",
		now,
		1,
	)
	if err != nil {
		t.Fatalf("insert imapsync: %v", err)
	}
	imapsyncID, _ := res.LastInsertId()

	// Insert event with correct container_id/name but wrong container_pk.
	_, err = dbConn.SQL.ExecContext(ctx, `
INSERT INTO events (container_pk, container_name, container_id, event_type, severity, message, ts, reason)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		victoriaID,
		"imapsync",
		"bbb222",
		"started",
		"blue",
		"Container started",
		now,
		"start",
	)
	if err != nil {
		t.Fatalf("insert event: %v", err)
	}

	st := New(dbConn.SQL)
	if err := st.Load(ctx); err != nil {
		t.Fatalf("load store: %v", err)
	}

	events, err := st.ListAllEvents(ctx, 0, 10)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	ev := events[0]
	if ev.ContainerPK != imapsyncID {
		t.Fatalf("expected container_pk %d, got %d", imapsyncID, ev.ContainerPK)
	}
	if ev.Container != "imapsync" {
		t.Fatalf("expected container_name imapsync, got %q", ev.Container)
	}
	if ev.ContainerID != "bbb222" {
		t.Fatalf("expected container_id bbb222, got %q", ev.ContainerID)
	}
}
