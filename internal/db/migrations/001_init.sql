CREATE TABLE IF NOT EXISTS schema_migrations (
  version INTEGER PRIMARY KEY,
  applied_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS containers (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE,
  container_id TEXT NOT NULL,
  image TEXT NOT NULL,
  image_tag TEXT NOT NULL,
  image_id TEXT NOT NULL,
  created_at_container TEXT NOT NULL,
  first_seen_at TEXT NOT NULL,
  status TEXT NOT NULL,
  caps TEXT NOT NULL,
  read_only INTEGER NOT NULL,
  user TEXT NOT NULL,
  last_event_id INTEGER,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  container_pk INTEGER NOT NULL,
  container_name TEXT NOT NULL,
  container_id TEXT NOT NULL,
  event_type TEXT NOT NULL,
  severity TEXT NOT NULL,
  message TEXT NOT NULL,
  ts TEXT NOT NULL,
  old_image TEXT,
  new_image TEXT,
  old_image_id TEXT,
  new_image_id TEXT,
  reason TEXT,
  details TEXT
);

CREATE INDEX IF NOT EXISTS idx_events_container_ts ON events(container_pk, ts DESC);
