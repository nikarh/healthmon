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
	db         *sql.DB
	mu         sync.RWMutex
	containers map[string]*Container
}

func New(db *sql.DB) *Store {
	return &Store{
		db:         db,
		containers: make(map[string]*Container),
	}
}

func (s *Store) Load(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.QueryContext(ctx, `SELECT id, name, container_id, image, image_tag, image_id, created_at_container, first_seen_at, status, role, caps, read_only, user, last_event_id, updated_at, present FROM containers`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var c Container
		var capsJSON string
		var readOnly int
		var present int
		var createdAt string
		var firstSeen string
		var updatedAt string
		var lastEventID sql.NullInt64

		if err := rows.Scan(&c.ID, &c.Name, &c.ContainerID, &c.Image, &c.ImageTag, &c.ImageID, &createdAt, &firstSeen, &c.Status, &c.Role, &capsJSON, &readOnly, &c.User, &lastEventID, &updatedAt, &present); err != nil {
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
		c.Present = present == 1
		if c.Role == "" {
			c.Role = "service"
		}
		container := c
		s.containers[container.Name] = &container
	}
	return rows.Err()
}

func (s *Store) ListContainers() []Container {
	s.mu.RLock()
	defer s.mu.RUnlock()

	items := make([]Container, 0, len(s.containers))
	for _, c := range s.containers {
		if !c.Present {
			continue
		}
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

func (s *Store) GetContainerByName(ctx context.Context, name string) (Container, bool, error) {
	s.mu.RLock()
	if c, ok := s.containers[name]; ok {
		clone := *c
		s.mu.RUnlock()
		return clone, true, nil
	}
	s.mu.RUnlock()

	var c Container
	var capsJSON string
	var readOnly int
	var present int
	var createdAt string
	var firstSeen string
	var updatedAt string
	var lastEventID sql.NullInt64

	err := s.db.QueryRowContext(ctx, `SELECT id, name, container_id, image, image_tag, image_id, created_at_container, first_seen_at, status, role, caps, read_only, user, last_event_id, updated_at, present FROM containers WHERE name = ?`, name).Scan(&c.ID, &c.Name, &c.ContainerID, &c.Image, &c.ImageTag, &c.ImageID, &createdAt, &firstSeen, &c.Status, &c.Role, &capsJSON, &readOnly, &c.User, &lastEventID, &updatedAt, &present)
	if err == sql.ErrNoRows {
		return Container{}, false, nil
	}
	if err != nil {
		return Container{}, false, err
	}
	if err := json.Unmarshal([]byte(capsJSON), &c.Caps); err != nil {
		return Container{}, false, err
	}
	c.ReadOnly = readOnly == 1
	c.CreatedAt = parseTime(createdAt)
	c.FirstSeenAt = parseTime(firstSeen)
	c.UpdatedAt = parseTime(updatedAt)
	if lastEventID.Valid {
		c.LastEventID = lastEventID.Int64
	}
	c.Present = present == 1
	if c.Role == "" {
		c.Role = "service"
	}
	s.mu.Lock()
	s.containers[c.Name] = &c
	s.mu.Unlock()
	return c, true, nil
}

func (s *Store) GetContainerByContainerID(ctx context.Context, containerID string) (Container, bool, error) {
	if containerID == "" {
		return Container{}, false, nil
	}

	s.mu.RLock()
	for _, c := range s.containers {
		if c.ContainerID == containerID {
			copy := *c
			s.mu.RUnlock()
			return copy, true, nil
		}
	}
	s.mu.RUnlock()

	var c Container
	var capsJSON string
	var readOnly int
	var present int
	var createdAt string
	var firstSeen string
	var updatedAt string
	var lastEventID sql.NullInt64

	err := s.db.QueryRowContext(ctx, `SELECT id, name, container_id, image, image_tag, image_id, created_at_container, first_seen_at, status, role, caps, read_only, user, last_event_id, updated_at, present FROM containers WHERE container_id = ?`, containerID).Scan(&c.ID, &c.Name, &c.ContainerID, &c.Image, &c.ImageTag, &c.ImageID, &createdAt, &firstSeen, &c.Status, &c.Role, &capsJSON, &readOnly, &c.User, &lastEventID, &updatedAt, &present)
	if err == sql.ErrNoRows {
		return Container{}, false, nil
	}
	if err != nil {
		return Container{}, false, err
	}
	if err := json.Unmarshal([]byte(capsJSON), &c.Caps); err != nil {
		return Container{}, false, err
	}
	c.ReadOnly = readOnly == 1
	c.CreatedAt = parseTime(createdAt)
	c.FirstSeenAt = parseTime(firstSeen)
	c.UpdatedAt = parseTime(updatedAt)
	if lastEventID.Valid {
		c.LastEventID = lastEventID.Int64
	}
	c.Present = present == 1
	if c.Role == "" {
		c.Role = "service"
	}
	s.mu.Lock()
	s.containers[c.Name] = &c
	s.mu.Unlock()
	return c, true, nil
}

func (s *Store) UpsertContainer(ctx context.Context, c Container) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if c.Role == "" {
		c.Role = "service"
	}
	if c.LastEventID == 0 {
		if existing, ok := s.containers[c.Name]; ok && existing.LastEventID > 0 {
			c.LastEventID = existing.LastEventID
		}
	}
	if !c.Present {
		c.Present = true
	}

	capsJSON, err := json.Marshal(c.Caps)
	if err != nil {
		return err
	}
	readOnly := 0
	if c.ReadOnly {
		readOnly = 1
	}
	present := 0
	if c.Present {
		present = 1
	}

	res, err := s.db.ExecContext(ctx, `
INSERT INTO containers (name, container_id, image, image_tag, image_id, created_at_container, first_seen_at, status, role, caps, read_only, user, last_event_id, updated_at, present)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(name) DO UPDATE SET
  container_id=excluded.container_id,
  image=excluded.image,
  image_tag=excluded.image_tag,
  image_id=excluded.image_id,
  created_at_container=excluded.created_at_container,
  status=excluded.status,
  role=excluded.role,
  caps=excluded.caps,
  read_only=excluded.read_only,
  user=excluded.user,
  last_event_id=excluded.last_event_id,
  updated_at=excluded.updated_at,
  present=excluded.present
`, c.Name, c.ContainerID, c.Image, c.ImageTag, c.ImageID, formatTime(c.CreatedAt), formatTime(c.FirstSeenAt), c.Status, c.Role, string(capsJSON), readOnly, c.User, nullInt(c.LastEventID), formatTime(c.UpdatedAt), present)
	if err != nil {
		return err
	}
	id := c.ID
	if id == 0 {
		if lastID, err := res.LastInsertId(); err == nil && lastID > 0 {
			id = lastID
		}
	}
	if id == 0 {
		if row := s.db.QueryRowContext(ctx, `SELECT id FROM containers WHERE name = ?`, c.Name); row != nil {
			var fetched int64
			if err := row.Scan(&fetched); err == nil {
				id = fetched
			}
		}
	}
	copy := c
	copy.ID = id
	s.containers[c.Name] = &copy
	return nil
}

func (s *Store) AddEvent(ctx context.Context, e Event) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
INSERT INTO events (container_pk, container_name, container_id, event_type, severity, message, ts, old_image, new_image, old_image_id, new_image_id, reason, details)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, e.ContainerPK, e.Container, e.ContainerID, e.Type, e.Severity, e.Message, formatTime(e.Timestamp), nullStr(e.OldImage), nullStr(e.NewImage), nullStr(e.OldImageID), nullStr(e.NewImageID), nullStr(e.Reason), nullStr(e.DetailsJSON))
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
	_, _ = s.db.ExecContext(ctx, `UPDATE containers SET last_event_id = ? WHERE id = ?`, id, e.ContainerPK)
	return id, nil
}

func (s *Store) ListEvents(ctx context.Context, container string, beforeID int64, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 50
	}
	if beforeID <= 0 {
		beforeID = int64(^uint64(0) >> 1)
	}

	containerInfo, ok, err := s.GetContainerByName(ctx, container)
	if err != nil {
		return nil, err
	}
	if !ok {
		return []Event{}, nil
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT id, container_name, container_id, event_type, severity, message, ts, old_image, new_image, old_image_id, new_image_id, reason, details, container_pk
FROM events
WHERE container_pk = ? AND id < ?
ORDER BY id DESC
LIMIT ?
`, containerInfo.ID, beforeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []Event{}
	for rows.Next() {
		var e Event
		var ts string
		var oldImage, newImage, oldImageID, newImageID, reason, details sql.NullString
		if err := rows.Scan(&e.ID, &e.Container, &e.ContainerID, &e.Type, &e.Severity, &e.Message, &ts, &oldImage, &newImage, &oldImageID, &newImageID, &reason, &details, &e.ContainerPK); err != nil {
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

func (s *Store) ListAllEvents(ctx context.Context, beforeID int64, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 50
	}
	if beforeID <= 0 {
		beforeID = int64(^uint64(0) >> 1)
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT id, container_name, container_id, event_type, severity, message, ts, old_image, new_image, old_image_id, new_image_id, reason, details, container_pk
FROM events
WHERE id < ?
ORDER BY id DESC
LIMIT ?
`, beforeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []Event{}
	for rows.Next() {
		var e Event
		var ts string
		var oldImage, newImage, oldImageID, newImageID, reason, details sql.NullString
		if err := rows.Scan(&e.ID, &e.Container, &e.ContainerID, &e.Type, &e.Severity, &e.Message, &ts, &oldImage, &newImage, &oldImageID, &newImageID, &reason, &details, &e.ContainerPK); err != nil {
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
SELECT id, container_name, container_id, event_type, severity, message, ts, old_image, new_image, old_image_id, new_image_id, reason, details, container_pk
FROM events
WHERE id = ?
`, id).Scan(&e.ID, &e.Container, &e.ContainerID, &e.Type, &e.Severity, &e.Message, &ts, &oldImage, &newImage, &oldImageID, &newImageID, &reason, &details, &e.ContainerPK)
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

func (s *Store) GetLatestEventByContainerPK(ctx context.Context, containerPK int64) (Event, bool, error) {
	if containerPK == 0 {
		return Event{}, false, nil
	}
	var e Event
	var ts string
	var oldImage, newImage, oldImageID, newImageID, reason, details sql.NullString
	err := s.db.QueryRowContext(ctx, `
SELECT id, container_name, container_id, event_type, severity, message, ts, old_image, new_image, old_image_id, new_image_id, reason, details, container_pk
FROM events
WHERE container_pk = ?
ORDER BY ts DESC
LIMIT 1
`, containerPK).Scan(&e.ID, &e.Container, &e.ContainerID, &e.Type, &e.Severity, &e.Message, &ts, &oldImage, &newImage, &oldImageID, &newImageID, &reason, &details, &e.ContainerPK)
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
		if !s.containers[name].Present {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (s *Store) DeleteContainer(ctx context.Context, name string) error {
	if name == "" {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE containers SET present = 0, updated_at = ? WHERE name = ?`, formatTime(time.Now().UTC()), name); err != nil {
		return err
	}
	s.mu.Lock()
	if c, ok := s.containers[name]; ok {
		c.Present = false
		c.UpdatedAt = time.Now().UTC()
	}
	s.mu.Unlock()
	return nil
}

func (s *Store) SetContainerPresent(ctx context.Context, name string, present bool) error {
	if name == "" {
		return nil
	}
	value := 0
	if present {
		value = 1
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE containers SET present = ?, updated_at = ? WHERE name = ?`, value, formatTime(time.Now().UTC()), name); err != nil {
		return err
	}
	s.mu.Lock()
	if c, ok := s.containers[name]; ok {
		c.Present = present
		c.UpdatedAt = time.Now().UTC()
	}
	s.mu.Unlock()
	return nil
}

func (s *Store) MarkAbsentExcept(ctx context.Context, presentNames map[string]struct{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for name, c := range s.containers {
		if _, ok := presentNames[name]; ok {
			if !c.Present {
				c.Present = true
				c.UpdatedAt = time.Now().UTC()
				_, _ = s.db.ExecContext(ctx, `UPDATE containers SET present = 1, updated_at = ? WHERE name = ?`, formatTime(c.UpdatedAt), name)
			}
			continue
		}
		if c.Present {
			c.Present = false
			c.UpdatedAt = time.Now().UTC()
			_, _ = s.db.ExecContext(ctx, `UPDATE containers SET present = 0, updated_at = ? WHERE name = ?`, formatTime(c.UpdatedAt), name)
		}
	}
	return nil
}

func (s *Store) RenameContainer(ctx context.Context, oldName, newName string, info Container) error {
	if oldName == "" || newName == "" || oldName == newName {
		return nil
	}

	s.mu.Lock()
	oldContainer, ok := s.containers[oldName]
	s.mu.Unlock()
	if !ok {
		if byID, byName := s.findContainerByID(info.ContainerID); byID != nil {
			oldContainer = byID
			oldName = byName
		} else {
			return nil
		}
	}

	info.Name = newName
	info.FirstSeenAt = oldContainer.FirstSeenAt
	info.LastEventID = oldContainer.LastEventID
	info.Present = true
	if info.Role == "" {
		info.Role = oldContainer.Role
	}

	s.mu.RLock()
	targetContainer, hasTarget := s.containers[newName]
	s.mu.RUnlock()

	if !hasTarget {
		if _, err := s.db.ExecContext(ctx, `UPDATE containers SET name = ?, container_id = ?, image = ?, image_tag = ?, image_id = ?, created_at_container = ?, first_seen_at = ?, status = ?, role = ?, caps = ?, read_only = ?, user = ?, last_event_id = ?, updated_at = ?, present = 1 WHERE name = ?`,
			newName,
			info.ContainerID,
			info.Image,
			info.ImageTag,
			info.ImageID,
			formatTime(info.CreatedAt),
			formatTime(info.FirstSeenAt),
			info.Status,
			info.Role,
			string(mustJSON(info.Caps)),
			boolToInt(info.ReadOnly),
			info.User,
			nullInt(info.LastEventID),
			formatTime(info.UpdatedAt),
			oldName,
		); err != nil {
			return err
		}
		_, _ = s.db.ExecContext(ctx, `UPDATE events SET container_name = ? WHERE container_pk = ?`, newName, oldContainer.ID)
		s.mu.Lock()
		delete(s.containers, oldName)
		info.ID = oldContainer.ID
		s.containers[newName] = &info
		s.mu.Unlock()
		return nil
	}

	if _, err := s.db.ExecContext(ctx, `UPDATE events SET container_pk = ?, container_name = ? WHERE container_pk = ?`, targetContainer.ID, newName, oldContainer.ID); err != nil {
		return err
	}

	if _, err := s.db.ExecContext(ctx, `UPDATE containers SET container_id = ?, image = ?, image_tag = ?, image_id = ?, created_at_container = ?, status = ?, role = ?, caps = ?, read_only = ?, user = ?, updated_at = ?, present = 1 WHERE id = ?`,
		info.ContainerID,
		info.Image,
		info.ImageTag,
		info.ImageID,
		formatTime(info.CreatedAt),
		info.Status,
		info.Role,
		string(mustJSON(info.Caps)),
		boolToInt(info.ReadOnly),
		info.User,
		formatTime(info.UpdatedAt),
		targetContainer.ID,
	); err != nil {
		return err
	}

	latestID, _ := s.latestEventID(ctx, targetContainer.ID)
	_, _ = s.db.ExecContext(ctx, `UPDATE containers SET last_event_id = ? WHERE id = ?`, nullInt(latestID), targetContainer.ID)

	_, _ = s.db.ExecContext(ctx, `UPDATE containers SET present = 0, updated_at = ? WHERE id = ?`, formatTime(time.Now().UTC()), oldContainer.ID)

	s.mu.Lock()
	if c, ok := s.containers[newName]; ok {
		c.ContainerID = info.ContainerID
		c.Image = info.Image
		c.ImageTag = info.ImageTag
		c.ImageID = info.ImageID
		c.CreatedAt = info.CreatedAt
		c.Status = info.Status
		c.Role = info.Role
		c.Caps = info.Caps
		c.ReadOnly = info.ReadOnly
		c.User = info.User
		c.UpdatedAt = info.UpdatedAt
		c.Present = true
		if latestID > 0 {
			c.LastEventID = latestID
		}
	}
	if c, ok := s.containers[oldName]; ok {
		c.Present = false
		c.UpdatedAt = time.Now().UTC()
		c.LastEventID = 0
	}
	s.mu.Unlock()
	return nil
}

func (s *Store) findContainerByID(id string) (*Container, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for name, c := range s.containers {
		if c.ContainerID == id {
			return c, name
		}
	}
	return nil, ""
}

func (s *Store) FindContainerByID(id string) (Container, string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for name, c := range s.containers {
		if c.ContainerID == id {
			copy := *c
			return copy, name, true
		}
	}
	return Container{}, "", false
}

func (s *Store) latestEventID(ctx context.Context, containerPK int64) (int64, error) {
	var id sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT MAX(id) FROM events WHERE container_pk = ?`, containerPK).Scan(&id); err != nil {
		return 0, err
	}
	if !id.Valid {
		return 0, nil
	}
	return id.Int64, nil
}

func boolToInt(val bool) int {
	if val {
		return 1
	}
	return 0
}

func mustJSON(val interface{}) []byte {
	raw, err := json.Marshal(val)
	if err != nil {
		return []byte("[]")
	}
	return raw
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
