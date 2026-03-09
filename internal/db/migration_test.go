package db

import (
	"context"
	"database/sql"
	"os"
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

func TestMigrateInfersServiceNamesFromRuntimeNames(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "healthmon.db")
	dbConn, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer dbConn.Close()

	if err := applyMigrationsUpTo(ctx, dbConn, 10); err != nil {
		t.Fatalf("apply base migrations: %v", err)
	}

	_, err = dbConn.SQL.ExecContext(ctx, `
INSERT INTO containers (
  id, name, container_id, current_container_name, image, image_tag, image_id, created_at_container, first_seen_at,
  status, caps, read_only, user, last_event_id, updated_at, role, present,
  registered_at, started_at, health_status, health_failing_streak, restart_loop,
  restart_streak, healthcheck, unhealthy_since, restart_loop_since,
  no_new_privileges, memory_reservation, memory_limit, finished_at, exit_code
) VALUES (
  10, '90e1683a21ff_healthmon', 'cid-healthmon', '90e1683a21ff_healthmon',
  'ghcr.io/example/healthmon', 'latest', 'sha256:healthmon',
  '2026-03-06T13:00:00Z', '2026-03-06T13:00:00Z', 'running', '[]', 1, '0:0',
  NULL, '2026-03-06T13:30:14Z', 'service', 1, '2026-03-06T13:00:00Z',
  '2026-03-06T13:00:00Z', '', 0, 0, 0, NULL, '0001-01-01T00:00:00Z',
  '0001-01-01T00:00:00Z', 1, 0, 0, NULL, NULL
);

INSERT INTO events (
  id, container_pk, container_name, container_id, parsed_container_name, event_type, severity, message, ts,
  old_image, new_image, old_image_id, new_image_id, reason, details, exit_code
) VALUES (
  100, 10, '90e1683a21ff_healthmon', 'cid-healthmon', '90e1683a21ff_healthmon',
  'created', 'blue', 'Container created', '2026-03-06T13:30:14Z',
  NULL, NULL, NULL, NULL, 'create', NULL, NULL
);

INSERT INTO alerts (
  id, container_pk, container_name, container_id, parsed_container_name, alert_type, severity, message, ts,
  old_image, new_image, old_image_id, new_image_id, reason, details, exit_code
) VALUES (
  200, 10, '90e1683a21ff_healthmon', 'cid-healthmon', '90e1683a21ff_healthmon',
  'recreated', 'blue', 'Container recreated', '2026-03-06T13:30:20Z',
  NULL, NULL, NULL, NULL, NULL, NULL, NULL
);
`)
	if err != nil {
		t.Fatalf("seed rows: %v", err)
	}

	if err := dbConn.Migrate(ctx); err != nil {
		t.Fatalf("run migration: %v", err)
	}

	var serviceName, runtimeName string
	if err := dbConn.SQL.QueryRowContext(ctx, `SELECT name, current_container_name FROM containers WHERE id = 10`).Scan(&serviceName, &runtimeName); err != nil {
		t.Fatalf("read migrated container: %v", err)
	}
	if serviceName != "healthmon" {
		t.Fatalf("expected inferred service name healthmon, got %q", serviceName)
	}
	if runtimeName != "90e1683a21ff_healthmon" {
		t.Fatalf("expected runtime container name to stay unchanged, got %q", runtimeName)
	}

	var eventName, eventParsed string
	if err := dbConn.SQL.QueryRowContext(ctx, `SELECT container_name, parsed_container_name FROM events WHERE id = 100`).Scan(&eventName, &eventParsed); err != nil {
		t.Fatalf("read migrated event: %v", err)
	}
	if eventName != "healthmon" {
		t.Fatalf("expected event service name healthmon, got %q", eventName)
	}
	if eventParsed != "90e1683a21ff_healthmon" {
		t.Fatalf("expected parsed event name to preserve runtime name, got %q", eventParsed)
	}

	var alertName, alertParsed string
	if err := dbConn.SQL.QueryRowContext(ctx, `SELECT container_name, parsed_container_name FROM alerts WHERE id = 200`).Scan(&alertName, &alertParsed); err != nil {
		t.Fatalf("read migrated alert: %v", err)
	}
	if alertName != "healthmon" {
		t.Fatalf("expected alert service name healthmon, got %q", alertName)
	}
	if alertParsed != "90e1683a21ff_healthmon" {
		t.Fatalf("expected parsed alert name to preserve runtime name, got %q", alertParsed)
	}
}

func TestMigrateDedupesContainersByContainerID(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "healthmon.db")
	dbConn, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer dbConn.Close()

	if err := applyMigrationsUpTo(ctx, dbConn, 11); err != nil {
		t.Fatalf("apply base migrations: %v", err)
	}

	_, err = dbConn.SQL.ExecContext(ctx, `
INSERT INTO containers (
  id, name, container_id, current_container_name, image, image_tag, image_id, created_at_container, first_seen_at,
  status, caps, read_only, user, last_event_id, updated_at, role, present,
  registered_at, started_at, health_status, health_failing_streak, restart_loop,
  restart_streak, healthcheck, unhealthy_since, restart_loop_since,
  no_new_privileges, memory_reservation, memory_limit, finished_at, exit_code
) VALUES
(
  10, 'imapsync', 'cid-imapsync', 'imapsync', 'docker.io/nikarh/fileserver-imapsync', 'latest', 'sha256:one',
  '2026-03-01T17:00:00Z', '2026-03-01T17:00:00Z', 'running', '[]', 1, '0:0', NULL, '2026-03-06T11:30:22Z',
  'service', 1, '2026-03-01T17:00:00Z', '2026-03-01T17:00:00Z', '', 0, 0, 0, NULL, '0001-01-01T00:00:00Z',
  '0001-01-01T00:00:00Z', 1, 0, 0, NULL, NULL
),
(
  20, '729d4232bd38_imapsync', 'cid-imapsync', '729d4232bd38_imapsync', 'docker.io/nikarh/fileserver-imapsync', 'latest', 'sha256:one',
  '2026-03-01T17:00:00Z', '2026-03-01T17:00:00Z', 'created', '[]', 1, '0:0', NULL, '2026-03-05T21:11:55Z',
  'service', 0, '2026-03-01T17:00:00Z', '2026-03-01T17:00:00Z', '', 0, 0, 0, NULL, '0001-01-01T00:00:00Z',
  '0001-01-01T00:00:00Z', 1, 0, 0, NULL, NULL
);

INSERT INTO events (
  id, container_pk, container_name, container_id, parsed_container_name, event_type, severity, message, ts,
  old_image, new_image, old_image_id, new_image_id, reason, details, exit_code
) VALUES (
  100, 20, '729d4232bd38_imapsync', 'cid-imapsync', '729d4232bd38_imapsync',
  'created', 'blue', 'Container created', '2026-03-05T21:11:50Z',
  NULL, NULL, NULL, NULL, 'create', NULL, NULL
);

INSERT INTO alerts (
  id, container_pk, container_name, container_id, parsed_container_name, alert_type, severity, message, ts,
  old_image, new_image, old_image_id, new_image_id, reason, details, exit_code
) VALUES (
  200, 20, '729d4232bd38_imapsync', 'cid-imapsync', '729d4232bd38_imapsync',
  'recreated', 'blue', 'Container recreated', '2026-03-05T21:11:55Z',
  NULL, NULL, NULL, NULL, NULL, NULL, NULL
);
`)
	if err != nil {
		t.Fatalf("seed duplicate rows: %v", err)
	}

	if err := dbConn.Migrate(ctx); err != nil {
		t.Fatalf("run migration: %v", err)
	}

	var count int
	if err := dbConn.SQL.QueryRowContext(ctx, `SELECT COUNT(1) FROM containers WHERE container_id = 'cid-imapsync'`).Scan(&count); err != nil {
		t.Fatalf("count containers: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one container row after dedupe, got %d", count)
	}

	var serviceName string
	if err := dbConn.SQL.QueryRowContext(ctx, `SELECT name FROM containers WHERE container_id = 'cid-imapsync'`).Scan(&serviceName); err != nil {
		t.Fatalf("read surviving container: %v", err)
	}
	if serviceName != "imapsync" {
		t.Fatalf("expected canonical service name imapsync, got %q", serviceName)
	}

	var eventPK int64
	var eventName string
	if err := dbConn.SQL.QueryRowContext(ctx, `SELECT container_pk, container_name FROM events WHERE id = 100`).Scan(&eventPK, &eventName); err != nil {
		t.Fatalf("read migrated event: %v", err)
	}
	if eventPK != 10 || eventName != "imapsync" {
		t.Fatalf("expected event to point at canonical service, got %q/%d", eventName, eventPK)
	}

	var alertPK int64
	var alertName string
	if err := dbConn.SQL.QueryRowContext(ctx, `SELECT container_pk, container_name FROM alerts WHERE id = 200`).Scan(&alertPK, &alertName); err != nil {
		t.Fatalf("read migrated alert: %v", err)
	}
	if alertPK != 10 || alertName != "imapsync" {
		t.Fatalf("expected alert to point at canonical service, got %q/%d", alertName, alertPK)
	}

	var indexName string
	if err := dbConn.SQL.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'index' AND name = 'idx_containers_container_id_unique'`).Scan(&indexName); err != nil {
		t.Fatalf("read unique index: %v", err)
	}
	if indexName != "idx_containers_container_id_unique" {
		t.Fatalf("expected unique index to be created, got %q", indexName)
	}
}

func TestMigrateRealDumpDedupesDuplicateContainerRows(t *testing.T) {
	ctx := context.Background()
	srcPath := filepath.Join("..", "..", "dump.db")
	dbPath := filepath.Join(t.TempDir(), "healthmon.db")

	data, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("read dump: %v", err)
	}
	if err := os.WriteFile(dbPath, data, 0o644); err != nil {
		t.Fatalf("write temp dump: %v", err)
	}

	dbConn, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer dbConn.Close()

	if err := dbConn.Migrate(ctx); err != nil {
		t.Fatalf("run migration: %v", err)
	}

	var duplicateCount int
	if err := dbConn.SQL.QueryRowContext(ctx, `SELECT COUNT(1) FROM (SELECT container_id FROM containers GROUP BY container_id HAVING COUNT(*) > 1)`).Scan(&duplicateCount); err != nil {
		t.Fatalf("count duplicate container ids: %v", err)
	}
	if duplicateCount != 0 {
		t.Fatalf("expected no duplicate container ids after migration, got %d", duplicateCount)
	}

	for _, removedName := range []string{
		"729d4232bd38_imapsync",
		"c7710be6ea4d_qbittorrent",
		"f0ff6885fbf2_filebrowser",
	} {
		var count int
		if err := dbConn.SQL.QueryRowContext(ctx, `SELECT COUNT(1) FROM containers WHERE name = ?`, removedName).Scan(&count); err != nil {
			t.Fatalf("count removed container %s: %v", removedName, err)
		}
		if count != 0 {
			t.Fatalf("expected %s to be removed, found %d rows", removedName, count)
		}
	}

	for _, expectedName := range []string{"imapsync", "qbittorrent", "filebrowser"} {
		var count int
		if err := dbConn.SQL.QueryRowContext(ctx, `SELECT COUNT(1) FROM containers WHERE name = ?`, expectedName).Scan(&count); err != nil {
			t.Fatalf("count canonical container %s: %v", expectedName, err)
		}
		if count != 1 {
			t.Fatalf("expected one %s row after migration, found %d", expectedName, count)
		}
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
