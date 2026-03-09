CREATE TEMP TABLE container_identity_scores AS
SELECT
  c.id,
  c.container_id,
  c.name,
  c.present,
  c.updated_at,
  COALESCE(e.event_count, 0) + COALESCE(a.alert_count, 0) AS history_count
FROM containers c
LEFT JOIN (
  SELECT container_pk, COUNT(*) AS event_count
  FROM events
  GROUP BY container_pk
) e ON e.container_pk = c.id
LEFT JOIN (
  SELECT container_pk, COUNT(*) AS alert_count
  FROM alerts
  GROUP BY container_pk
) a ON a.container_pk = c.id;

CREATE TEMP TABLE container_identity_keepers AS
WITH ranked AS (
  SELECT
    id,
    container_id,
    name,
    ROW_NUMBER() OVER (
      PARTITION BY container_id
      ORDER BY present DESC, history_count DESC, updated_at DESC, id ASC
    ) AS row_num,
    COUNT(*) OVER (PARTITION BY container_id) AS row_count
  FROM container_identity_scores
)
SELECT container_id, id AS keep_id, name AS keep_name
FROM ranked
WHERE row_count > 1
  AND row_num = 1;

CREATE TEMP TABLE container_identity_duplicates AS
WITH ranked AS (
  SELECT
    id,
    container_id,
    ROW_NUMBER() OVER (
      PARTITION BY container_id
      ORDER BY present DESC, history_count DESC, updated_at DESC, id ASC
    ) AS row_num,
    COUNT(*) OVER (PARTITION BY container_id) AS row_count
  FROM container_identity_scores
)
SELECT ranked.id AS duplicate_id, ranked.container_id, keepers.keep_id
FROM ranked
JOIN container_identity_keepers keepers ON keepers.container_id = ranked.container_id
WHERE ranked.row_count > 1
  AND ranked.row_num > 1;

UPDATE events
SET container_pk = (
      SELECT keep_id
      FROM container_identity_duplicates d
      WHERE d.duplicate_id = events.container_pk
    ),
    container_name = (
      SELECT k.keep_name
      FROM container_identity_duplicates d
      JOIN container_identity_keepers k ON k.container_id = d.container_id
      WHERE d.duplicate_id = events.container_pk
    )
WHERE container_pk IN (SELECT duplicate_id FROM container_identity_duplicates);

UPDATE alerts
SET container_pk = (
      SELECT keep_id
      FROM container_identity_duplicates d
      WHERE d.duplicate_id = alerts.container_pk
    ),
    container_name = (
      SELECT k.keep_name
      FROM container_identity_duplicates d
      JOIN container_identity_keepers k ON k.container_id = d.container_id
      WHERE d.duplicate_id = alerts.container_pk
    )
WHERE container_pk IN (SELECT duplicate_id FROM container_identity_duplicates);

UPDATE events
SET container_name = (
  SELECT keep_name
  FROM container_identity_keepers k
  WHERE k.container_id = events.container_id
)
WHERE container_id IN (SELECT container_id FROM container_identity_keepers)
  AND container_name <> (
    SELECT keep_name
    FROM container_identity_keepers k
    WHERE k.container_id = events.container_id
  );

UPDATE alerts
SET container_name = (
  SELECT keep_name
  FROM container_identity_keepers k
  WHERE k.container_id = alerts.container_id
)
WHERE container_id IN (SELECT container_id FROM container_identity_keepers)
  AND container_name <> (
    SELECT keep_name
    FROM container_identity_keepers k
    WHERE k.container_id = alerts.container_id
  );

DELETE FROM containers
WHERE id IN (SELECT duplicate_id FROM container_identity_duplicates);

DROP TABLE container_identity_duplicates;
DROP TABLE container_identity_keepers;
DROP TABLE container_identity_scores;

CREATE UNIQUE INDEX IF NOT EXISTS idx_containers_container_id_unique
ON containers(container_id);
