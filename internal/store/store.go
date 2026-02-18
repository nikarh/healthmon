package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"sync"
	"time"
)

type Store struct {
	db              *sql.DB
	mu              sync.RWMutex
	containers      map[string]*Container
	eventCacheLimit int
}

func New(db *sql.DB, eventCacheLimit int) *Store {
	return &Store{
		db:              db,
		containers:      make(map[string]*Container),
		eventCacheLimit: eventCacheLimit,
	}
}

func (s *Store) Load(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.QueryContext(ctx, `SELECT name, container_id, image, image_tag, image_id, created_at_container, first_seen_at, status, caps, read_only, user, last_event_id, updated_at FROM containers`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var c Container
		var capsJSON string
		var readOnly int
		var createdAt string
		var firstSeen string
		var updatedAt string
		var lastEventID sql.NullInt64

		if err := rows.Scan(&c.Name, &c.ContainerID, &c.Image, &c.ImageTag, &c.ImageID, &createdAt, &firstSeen, &c.Status, &capsJSON, &readOnly, &c.User, &lastEventID, &updatedAt); err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(capsJSON), &c.Caps); err != nil {
			return err
		}
		c.ReadOnly = readOnly == 1
		c.CreatedAt = parseTime(createdAt)
		c.FirstSeenAt = parseTime(firstSeen)
		c.UpdatedAt = parseTime(updatedAt)
		if lastEventID.Valid {
			c.LastEventID = lastEventID.Int64
		}
		s.containers[c.Name] = &c
	}
	return rows.Err()
}

func (s *Store) ListContainers() []Container {
	s.mu.RLock()
	defer s.mu.RUnlock()

	items := make([]Container, 0, len(s.containers))
	for _, c := range s.containers {
		items = append(items, *c)
	}
	return items
}

func (s *Store) GetContainer(name string) (Container, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.containers[name]
	if !ok {
		return Container{}, false
	}
	return *c, true
}

func (s *Store) UpsertContainer(ctx context.Context, c Container) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	capsJSON, err := json.Marshal(c.Caps)
	if err != nil {
		return err
	}
	readOnly := 0
	if c.ReadOnly {
		readOnly = 1
	}

	_, err = s.db.ExecContext(ctx, `
INSERT INTO containers (name, container_id, image, image_tag, image_id, created_at_container, first_seen_at, status, caps, read_only, user, last_event_id, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(name) DO UPDATE SET
  container_id=excluded.container_id,
  image=excluded.image,
  image_tag=excluded.image_tag,
  image_id=excluded.image_id,
  created_at_container=excluded.created_at_container,
  status=excluded.status,
  caps=excluded.caps,
  read_only=excluded.read_only,
  user=excluded.user,
  last_event_id=excluded.last_event_id,
  updated_at=excluded.updated_at
`, c.Name, c.ContainerID, c.Image, c.ImageTag, c.ImageID, formatTime(c.CreatedAt), formatTime(c.FirstSeenAt), c.Status, string(capsJSON), readOnly, c.User, nullInt(c.LastEventID), formatTime(c.UpdatedAt))
	if err != nil {
		return err
	}
	copy := c
	s.containers[c.Name] = &copy
	return nil
}

func (s *Store) AddEvent(ctx context.Context, e Event) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
INSERT INTO events (container_name, container_id, event_type, severity, message, ts, old_image, new_image, old_image_id, new_image_id, reason, details)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, e.Container, e.ContainerID, e.Type, e.Severity, e.Message, formatTime(e.Timestamp), nullStr(e.OldImage), nullStr(e.NewImage), nullStr(e.OldImageID), nullStr(e.NewImageID), nullStr(e.Reason), nullStr(e.DetailsJSON))
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	e.ID = id
	s.mu.Lock()
	if c, ok := s.containers[e.Container]; ok {
		c.LastEventID = id
		c.UpdatedAt = e.Timestamp
	}
	s.mu.Unlock()
	return id, nil
}

func (s *Store) ListEvents(ctx context.Context, container string, beforeID int64, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 50
	}
	if beforeID <= 0 {
		beforeID = int64(^uint64(0) >> 1)
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT id, container_name, container_id, event_type, severity, message, ts, old_image, new_image, old_image_id, new_image_id, reason, details
FROM events
WHERE container_name = ? AND id < ?
ORDER BY id DESC
LIMIT ?
`, container, beforeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []Event{}
	for rows.Next() {
		var e Event
		var ts string
		var oldImage, newImage, oldImageID, newImageID, reason, details sql.NullString
		if err := rows.Scan(&e.ID, &e.Container, &e.ContainerID, &e.Type, &e.Severity, &e.Message, &ts, &oldImage, &newImage, &oldImageID, &newImageID, &reason, &details); err != nil {
			return nil, err
		}
		e.Timestamp = parseTime(ts)
		if oldImage.Valid {
			e.OldImage = oldImage.String
		}
		if newImage.Valid {
			e.NewImage = newImage.String
		}
		if oldImageID.Valid {
			e.OldImageID = oldImageID.String
		}
		if newImageID.Valid {
			e.NewImageID = newImageID.String
		}
		if reason.Valid {
			e.Reason = reason.String
		}
		if details.Valid {
			e.DetailsJSON = details.String
		}
		items = append(items, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Store) GetEvent(ctx context.Context, id int64) (Event, bool, error) {
	if id <= 0 {
		return Event{}, false, nil
	}
	var e Event
	var ts string
	var oldImage, newImage, oldImageID, newImageID, reason, details sql.NullString
	err := s.db.QueryRowContext(ctx, `
SELECT id, container_name, container_id, event_type, severity, message, ts, old_image, new_image, old_image_id, new_image_id, reason, details
FROM events
WHERE id = ?
`, id).Scan(&e.ID, &e.Container, &e.ContainerID, &e.Type, &e.Severity, &e.Message, &ts, &oldImage, &newImage, &oldImageID, &newImageID, &reason, &details)
	if err == sql.ErrNoRows {
		return Event{}, false, nil
	}
	if err != nil {
		return Event{}, false, err
	}
	e.Timestamp = parseTime(ts)
	if oldImage.Valid {
		e.OldImage = oldImage.String
	}
	if newImage.Valid {
		e.NewImage = newImage.String
	}
	if oldImageID.Valid {
		e.OldImageID = oldImageID.String
	}
	if newImageID.Valid {
		e.NewImageID = newImageID.String
	}
	if reason.Valid {
		e.Reason = reason.String
	}
	if details.Valid {
		e.DetailsJSON = details.String
	}
	return e, true, nil
}

func (s *Store) ContainerNames() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := make([]string, 0, len(s.containers))
	for name := range s.containers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func nullStr(val string) interface{} {
	if val == "" {
		return nil
	}
	return val
}

func nullInt(val int64) interface{} {
	if val == 0 {
		return nil
	}
	return val
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return time.Time{}.UTC().Format(time.RFC3339)
	}
	return t.UTC().Format(time.RFC3339)
}

func parseTime(val string) time.Time {
	if val == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339, val)
	if err != nil {
		return time.Time{}
	}
	return parsed
}
