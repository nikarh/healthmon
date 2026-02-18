CREATE TABLE IF NOT EXISTS containers_new (
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

CREATE TABLE IF NOT EXISTS events_new (
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

INSERT INTO containers_new (name, container_id, image, image_tag, image_id, created_at_container, first_seen_at, status, caps, read_only, user, last_event_id, updated_at)
SELECT name, container_id, image, image_tag, image_id, created_at_container, first_seen_at, status, caps, read_only, user, last_event_id, updated_at
FROM containers;

INSERT INTO events_new (id, container_pk, container_name, container_id, event_type, severity, message, ts, old_image, new_image, old_image_id, new_image_id, reason, details)
SELECT e.id, c.id, e.container_name, e.container_id, e.event_type, e.severity, e.message, e.ts, e.old_image, e.new_image, e.old_image_id, e.new_image_id, e.reason, e.details
FROM events e
JOIN containers_new c ON c.name = e.container_name;

DROP TABLE events;
DROP TABLE containers;

ALTER TABLE containers_new RENAME TO containers;
ALTER TABLE events_new RENAME TO events;

CREATE INDEX IF NOT EXISTS idx_events_container_ts ON events(container_pk, ts DESC);
