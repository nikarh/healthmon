ALTER TABLE containers ADD COLUMN current_container_name TEXT NOT NULL DEFAULT '';

ALTER TABLE events ADD COLUMN parsed_container_name TEXT;
ALTER TABLE alerts ADD COLUMN parsed_container_name TEXT;

UPDATE containers
SET current_container_name = CASE
  WHEN current_container_name IS NULL OR current_container_name = '' THEN name
  ELSE current_container_name
END;

UPDATE events
SET parsed_container_name = CASE
  WHEN parsed_container_name IS NULL OR parsed_container_name = '' THEN container_name
  ELSE parsed_container_name
END;

UPDATE alerts
SET parsed_container_name = CASE
  WHEN parsed_container_name IS NULL OR parsed_container_name = '' THEN container_name
  ELSE parsed_container_name
END;
