WITH candidates AS (
  SELECT
    id,
    name AS old_name,
    current_container_name,
    CASE
      WHEN instr(current_container_name, '_') > 1
        AND length(substr(current_container_name, 1, instr(current_container_name, '_') - 1)) >= 12
        AND length(
          replace(replace(replace(replace(replace(replace(replace(replace(replace(replace(replace(replace(replace(replace(replace(replace(
            lower(substr(current_container_name, 1, instr(current_container_name, '_') - 1)),
          '0', ''), '1', ''), '2', ''), '3', ''), '4', ''), '5', ''), '6', ''), '7', ''), '8', ''), '9', ''),
          'a', ''), 'b', ''), 'c', ''), 'd', ''), 'e', ''), 'f', '')
        ) = 0
        AND length(substr(current_container_name, instr(current_container_name, '_') + 1)) > 0
      THEN substr(current_container_name, instr(current_container_name, '_') + 1)
      ELSE ''
    END AS inferred_name
  FROM containers
),
renames AS (
  SELECT c.id, c.old_name, c.inferred_name
  FROM candidates c
  WHERE c.inferred_name <> ''
    AND c.inferred_name <> c.old_name
    AND NOT EXISTS (
      SELECT 1
      FROM containers existing
      WHERE existing.name = c.inferred_name
        AND existing.id <> c.id
    )
)
UPDATE containers
SET name = (
  SELECT renames.inferred_name
  FROM renames
  WHERE renames.id = containers.id
)
WHERE id IN (SELECT id FROM renames);

UPDATE events
SET container_name = (
  SELECT c.name
  FROM containers c
  WHERE c.id = events.container_pk
)
WHERE container_pk > 0
  AND EXISTS (
    SELECT 1
    FROM containers c
    WHERE c.id = events.container_pk
      AND events.container_name <> c.name
  );

UPDATE events
SET container_name = (
  SELECT c.name
  FROM containers c
  WHERE c.container_id = events.container_id
)
WHERE events.container_id <> ''
  AND EXISTS (
    SELECT 1
    FROM containers c
    WHERE c.container_id = events.container_id
      AND events.container_name <> c.name
  );

UPDATE alerts
SET container_name = (
  SELECT c.name
  FROM containers c
  WHERE c.id = alerts.container_pk
)
WHERE container_pk > 0
  AND EXISTS (
    SELECT 1
    FROM containers c
    WHERE c.id = alerts.container_pk
      AND alerts.container_name <> c.name
  );

UPDATE alerts
SET container_name = (
  SELECT c.name
  FROM containers c
  WHERE c.container_id = alerts.container_id
)
WHERE alerts.container_id <> ''
  AND EXISTS (
    SELECT 1
    FROM containers c
    WHERE c.container_id = alerts.container_id
      AND alerts.container_name <> c.name
  );
