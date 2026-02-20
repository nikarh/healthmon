ALTER TABLE containers ADD COLUMN registered_at TEXT NOT NULL DEFAULT '0001-01-01T00:00:00Z';
ALTER TABLE containers ADD COLUMN started_at TEXT NOT NULL DEFAULT '0001-01-01T00:00:00Z';
ALTER TABLE containers ADD COLUMN health_status TEXT NOT NULL DEFAULT '';
ALTER TABLE containers ADD COLUMN health_failing_streak INTEGER NOT NULL DEFAULT 0;
ALTER TABLE containers ADD COLUMN restart_loop INTEGER NOT NULL DEFAULT 0;
ALTER TABLE containers ADD COLUMN restart_streak INTEGER NOT NULL DEFAULT 0;
ALTER TABLE containers ADD COLUMN healthcheck TEXT;

ALTER TABLE events ADD COLUMN exit_code INTEGER;

CREATE TABLE IF NOT EXISTS alerts (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  container_pk INTEGER NOT NULL,
  container_name TEXT NOT NULL,
  container_id TEXT NOT NULL,
  alert_type TEXT NOT NULL,
  severity TEXT NOT NULL,
  message TEXT NOT NULL,
  ts TEXT NOT NULL,
  old_image TEXT,
  new_image TEXT,
  old_image_id TEXT,
  new_image_id TEXT,
  reason TEXT,
  details TEXT,
  exit_code INTEGER
);

CREATE INDEX IF NOT EXISTS idx_alerts_container_ts ON alerts(container_pk, ts DESC);
CREATE INDEX IF NOT EXISTS idx_alerts_ts ON alerts(ts DESC);

UPDATE containers
SET registered_at = CASE
  WHEN registered_at IS NULL OR registered_at = '' OR registered_at = '0001-01-01T00:00:00Z' THEN
    CASE
      WHEN created_at_container < first_seen_at THEN created_at_container
      ELSE first_seen_at
    END
  ELSE registered_at
END;

UPDATE containers
SET started_at = CASE
  WHEN started_at IS NULL OR started_at = '' OR started_at = '0001-01-01T00:00:00Z' THEN created_at_container
  ELSE started_at
END;
