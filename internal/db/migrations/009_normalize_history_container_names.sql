UPDATE events
SET
  container_pk = (
    SELECT c.id
    FROM containers c
    WHERE c.container_id = events.container_id
  ),
  container_name = (
    SELECT c.name
    FROM containers c
    WHERE c.container_id = events.container_id
  )
WHERE events.container_id <> ''
  AND EXISTS (
    SELECT 1
    FROM containers c
    WHERE c.container_id = events.container_id
      AND (events.container_pk <> c.id OR events.container_name <> c.name)
  );

UPDATE alerts
SET
  container_pk = (
    SELECT c.id
    FROM containers c
    WHERE c.container_id = alerts.container_id
  ),
  container_name = (
    SELECT c.name
    FROM containers c
    WHERE c.container_id = alerts.container_id
  )
WHERE alerts.container_id <> ''
  AND EXISTS (
    SELECT 1
    FROM containers c
    WHERE c.container_id = alerts.container_id
      AND (alerts.container_pk <> c.id OR alerts.container_name <> c.name)
  );
