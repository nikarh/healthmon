ALTER TABLE containers ADD COLUMN service_key TEXT NOT NULL DEFAULT '';
ALTER TABLE containers ADD COLUMN compose_service TEXT NOT NULL DEFAULT '';
ALTER TABLE containers ADD COLUMN compose_project TEXT NOT NULL DEFAULT '';
ALTER TABLE containers ADD COLUMN compose_workdir TEXT NOT NULL DEFAULT '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_containers_service_key_unique
ON containers(service_key)
WHERE service_key <> '';
