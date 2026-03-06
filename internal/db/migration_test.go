package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMigrateNormalizesHistoricalContainerNames(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "healthmon.db")
	dbConn, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer dbConn.Close()

	if err := applyMigrationsUpTo(ctx, dbConn, 8); err != nil {
		t.Fatalf("apply base migrations: %v", err)
	}

	_, err = dbConn.SQL.ExecContext(ctx, `
INSERT INTO containers (
  id, name, container_id, image, image_tag, image_id, created_at_container, first_seen_at,
  status, caps, read_only, user, last_event_id, updated_at, role, present,
  registered_at, started_at, health_status, health_failing_streak, restart_loop,
  restart_streak, healthcheck, unhealthy_since, restart_loop_since,
  no_new_privileges, memory_reservation, memory_limit, finished_at, exit_code
) VALUES (
  10, 'affine', 'cid-rename', 'ghcr.io/example/affine', 'stable', 'sha256:image',
  '2026-03-01T17:00:00Z', '2026-03-01T17:00:00Z', 'running', '[]', 1, '1000:1000',
  NULL, '2026-03-01T19:10:08Z', 'service', 1, '2026-03-01T17:00:00Z',
  '2026-03-01T17:00:00Z', '', 0, 0, 0, NULL, '0001-01-01T00:00:00Z',
  '0001-01-01T00:00:00Z', 1, 0, 0, NULL, NULL
);

INSERT INTO events (
  id, container_pk, container_name, container_id, event_type, severity, message, ts,
  old_image, new_image, old_image_id, new_image_id, reason, details, exit_code
) VALUES (
  100, 99, 'elastic_ride', 'cid-rename', 'restart', 'blue', 'Restart event: die',
  '2026-03-01T19:09:58Z', NULL, NULL, NULL, NULL, 'die', NULL, NULL
);

INSERT INTO alerts (
  id, container_pk, container_name, container_id, alert_type, severity, message, ts,
  old_image, new_image, old_image_id, new_image_id, reason, details, exit_code
) VALUES (
  200, 99, 'elastic_ride', 'cid-rename', 'restart_loop', 'red', 'Restart loop detected',
  '2026-03-01T17:23:39Z', NULL, NULL, NULL, NULL, NULL, NULL, NULL
);
`)
	if err != nil {
		t.Fatalf("seed historical rows: %v", err)
	}

	if err := dbConn.Migrate(ctx); err != nil {
		t.Fatalf("run migration: %v", err)
	}

	var eventPK int64
	var eventName string
	if err := dbConn.SQL.QueryRowContext(ctx, `SELECT container_pk, container_name FROM events WHERE id = 100`).Scan(&eventPK, &eventName); err != nil {
		t.Fatalf("read migrated event: %v", err)
	}
	if eventPK != 10 || eventName != "affine" {
		t.Fatalf("expected migrated event to point at affine/10, got %q/%d", eventName, eventPK)
	}

	var alertPK int64
	var alertName string
	if err := dbConn.SQL.QueryRowContext(ctx, `SELECT container_pk, container_name FROM alerts WHERE id = 200`).Scan(&alertPK, &alertName); err != nil {
		t.Fatalf("read migrated alert: %v", err)
	}
	if alertPK != 10 || alertName != "affine" {
		t.Fatalf("expected migrated alert to point at affine/10, got %q/%d", alertName, alertPK)
	}
}

func TestMigrateAddsServiceIdentityColumns(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "healthmon.db")
	dbConn, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer dbConn.Close()

	if err := applyMigrationsUpTo(ctx, dbConn, 9); err != nil {
		t.Fatalf("apply base migrations: %v", err)
	}

	_, err = dbConn.SQL.ExecContext(ctx, `
INSERT INTO containers (
  id, name, container_id, image, image_tag, image_id, created_at_container, first_seen_at,
  status, caps, read_only, user, last_event_id, updated_at, role, present,
  registered_at, started_at, health_status, health_failing_streak, restart_loop,
  restart_streak, healthcheck, unhealthy_since, restart_loop_since,
  no_new_privileges, memory_reservation, memory_limit, finished_at, exit_code
) VALUES (
  10, 'affine', 'cid-rename', 'ghcr.io/example/affine', 'stable', 'sha256:image',
  '2026-03-01T17:00:00Z', '2026-03-01T17:00:00Z', 'running', '[]', 1, '1000:1000',
  NULL, '2026-03-01T19:10:08Z', 'service', 1, '2026-03-01T17:00:00Z',
  '2026-03-01T17:00:00Z', '', 0, 0, 0, NULL, '0001-01-01T00:00:00Z',
  '0001-01-01T00:00:00Z', 1, 0, 0, NULL, NULL
);

INSERT INTO events (
  id, container_pk, container_name, container_id, event_type, severity, message, ts,
  old_image, new_image, old_image_id, new_image_id, reason, details, exit_code
) VALUES (
  100, 10, 'elastic_ride', 'cid-rename', 'restart', 'blue', 'Restart event: die',
  '2026-03-01T19:09:58Z', NULL, NULL, NULL, NULL, 'die', NULL, NULL
);

INSERT INTO alerts (
  id, container_pk, container_name, container_id, alert_type, severity, message, ts,
  old_image, new_image, old_image_id, new_image_id, reason, details, exit_code
) VALUES (
  200, 10, 'elastic_ride', 'cid-rename', 'restart_loop', 'red', 'Restart loop detected',
  '2026-03-01T17:23:39Z', NULL, NULL, NULL, NULL, NULL, NULL, NULL
);
`)
	if err != nil {
		t.Fatalf("seed rows: %v", err)
	}

	if err := dbConn.Migrate(ctx); err != nil {
		t.Fatalf("run migration: %v", err)
	}

	var currentContainerName string
	if err := dbConn.SQL.QueryRowContext(ctx, `SELECT current_container_name FROM containers WHERE id = 10`).Scan(&currentContainerName); err != nil {
		t.Fatalf("read container current name: %v", err)
	}
	if currentContainerName != "affine" {
		t.Fatalf("expected current container name affine, got %q", currentContainerName)
	}

	var parsedEventName sql.NullString
	if err := dbConn.SQL.QueryRowContext(ctx, `SELECT parsed_container_name FROM events WHERE id = 100`).Scan(&parsedEventName); err != nil {
		t.Fatalf("read parsed event name: %v", err)
	}
	if !parsedEventName.Valid || parsedEventName.String != "elastic_ride" {
		t.Fatalf("expected parsed event name elastic_ride, got %#v", parsedEventName)
	}

	var parsedAlertName sql.NullString
	if err := dbConn.SQL.QueryRowContext(ctx, `SELECT parsed_container_name FROM alerts WHERE id = 200`).Scan(&parsedAlertName); err != nil {
		t.Fatalf("read parsed alert name: %v", err)
	}
	if !parsedAlertName.Valid || parsedAlertName.String != "elastic_ride" {
		t.Fatalf("expected parsed alert name elastic_ride, got %#v", parsedAlertName)
	}
}

func applyMigrationsUpTo(ctx context.Context, dbConn *DB, maxVersion int) error {
	if _, err := dbConn.SQL.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return err
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		parts := strings.Split(entry.Name(), "_")
		version, err := strconv.Atoi(parts[0])
		if err != nil {
			return err
		}
		if version > maxVersion {
			continue
		}
		content, err := migrationsFS.ReadFile(filepath.Join("migrations", entry.Name()))
		if err != nil {
			return err
		}
		if _, err := dbConn.SQL.ExecContext(ctx, string(content)); err != nil {
			return err
		}
		if _, err := dbConn.SQL.ExecContext(ctx, `INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`, version, time.Now().UTC().Format(time.RFC3339)); err != nil {
			return err
		}
	}
	return nil
}
