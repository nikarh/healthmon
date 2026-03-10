package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"strings"
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

	rows, err := s.db.QueryContext(ctx, `SELECT id, name, service_key, compose_service, compose_project, compose_workdir, container_id, current_container_name, image, image_tag, image_id, created_at_container, registered_at, started_at, finished_at, exit_code, status, role, caps, read_only, no_new_privileges, memory_reservation, memory_limit, user, last_event_id, updated_at, present, health_status, health_failing_streak, unhealthy_since, restart_loop, restart_streak, restart_loop_since, healthcheck FROM containers`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var c Container
		var capsJSON string
		var readOnly int
		var noNewPrivileges int
		var memoryReservation int64
		var memoryLimit int64
		var present int
		var createdAt string
		var registeredAt string
		var startedAt string
		var finishedAt sql.NullString
		var exitCode sql.NullInt64
		var updatedAt string
		var lastEventID sql.NullInt64
		var healthStatus string
		var healthFailingStreak int
		var unhealthySince string
		var restartLoop int
		var restartStreak int
		var restartLoopSince string
		var healthcheck sql.NullString

		if err := rows.Scan(&c.ID, &c.Name, &c.ServiceKey, &c.ComposeService, &c.ComposeProject, &c.ComposeWorkdir, &c.ContainerID, &c.CurrentContainerName, &c.Image, &c.ImageTag, &c.ImageID, &createdAt, &registeredAt, &startedAt, &finishedAt, &exitCode, &c.Status, &c.Role, &capsJSON, &readOnly, &noNewPrivileges, &memoryReservation, &memoryLimit, &c.User, &lastEventID, &updatedAt, &present, &healthStatus, &healthFailingStreak, &unhealthySince, &restartLoop, &restartStreak, &restartLoopSince, &healthcheck); err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(capsJSON), &c.Caps); err != nil {
			return err
		}
		c.ReadOnly = readOnly == 1
		c.NoNewPrivileges = noNewPrivileges == 1
		c.MemoryReservation = memoryReservation
		c.MemoryLimit = memoryLimit
		c.CreatedAt = parseTime(createdAt)
		c.RegisteredAt = parseTime(registeredAt)
		c.StartedAt = parseTime(startedAt)
		if finishedAt.Valid {
			c.FinishedAt = parseTime(finishedAt.String)
		}
		if exitCode.Valid {
			val := int(exitCode.Int64)
			c.ExitCode = &val
		}
		c.UpdatedAt = parseTime(updatedAt)
		if lastEventID.Valid {
			c.LastEventID = lastEventID.Int64
		}
		c.Present = present == 1
		c.HealthStatus = healthStatus
		c.HealthFailingStreak = healthFailingStreak
		c.UnhealthySince = parseTime(unhealthySince)
		c.RestartLoop = restartLoop == 1
		c.RestartStreak = restartStreak
		c.RestartLoopSince = parseTime(restartLoopSince)
		if parsed, err := parseHealthcheck(healthcheck); err != nil {
			return err
		} else {
			c.Healthcheck = parsed
		}
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
	var memoryReservation int64
	var memoryLimit int64
	var createdAt string
	var registeredAt string
	var startedAt string
	var updatedAt string
	var lastEventID sql.NullInt64
	var healthStatus string
	var healthFailingStreak int
	var unhealthySince string
	var restartLoop int
	var restartStreak int
	var restartLoopSince string
	var healthcheck sql.NullString

	var noNewPrivileges int
	var finishedAt sql.NullString
	var exitCode sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT id, name, service_key, compose_service, compose_project, compose_workdir, container_id, current_container_name, image, image_tag, image_id, created_at_container, registered_at, started_at, finished_at, exit_code, status, role, caps, read_only, no_new_privileges, memory_reservation, memory_limit, user, last_event_id, updated_at, present, health_status, health_failing_streak, unhealthy_since, restart_loop, restart_streak, restart_loop_since, healthcheck FROM containers WHERE name = ?`, name).Scan(&c.ID, &c.Name, &c.ServiceKey, &c.ComposeService, &c.ComposeProject, &c.ComposeWorkdir, &c.ContainerID, &c.CurrentContainerName, &c.Image, &c.ImageTag, &c.ImageID, &createdAt, &registeredAt, &startedAt, &finishedAt, &exitCode, &c.Status, &c.Role, &capsJSON, &readOnly, &noNewPrivileges, &memoryReservation, &memoryLimit, &c.User, &lastEventID, &updatedAt, &present, &healthStatus, &healthFailingStreak, &unhealthySince, &restartLoop, &restartStreak, &restartLoopSince, &healthcheck)
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
	c.NoNewPrivileges = noNewPrivileges == 1
	c.MemoryReservation = memoryReservation
	c.MemoryLimit = memoryLimit
	c.CreatedAt = parseTime(createdAt)
	c.RegisteredAt = parseTime(registeredAt)
	c.StartedAt = parseTime(startedAt)
	if finishedAt.Valid {
		c.FinishedAt = parseTime(finishedAt.String)
	}
	if exitCode.Valid {
		val := int(exitCode.Int64)
		c.ExitCode = &val
	}
	c.UpdatedAt = parseTime(updatedAt)
	if lastEventID.Valid {
		c.LastEventID = lastEventID.Int64
	}
	c.Present = present == 1
	c.HealthStatus = healthStatus
	c.HealthFailingStreak = healthFailingStreak
	c.UnhealthySince = parseTime(unhealthySince)
	c.RestartLoop = restartLoop == 1
	c.RestartStreak = restartStreak
	c.RestartLoopSince = parseTime(restartLoopSince)
	if parsed, err := parseHealthcheck(healthcheck); err != nil {
		return Container{}, false, err
	} else {
		c.Healthcheck = parsed
	}
	if c.Role == "" {
		c.Role = "service"
	}
	s.mu.Lock()
	s.containers[c.Name] = &c
	s.mu.Unlock()
	return c, true, nil
}

func (s *Store) GetContainerByServiceKey(ctx context.Context, serviceKey string) (Container, bool, error) {
	if strings.TrimSpace(serviceKey) == "" {
		return Container{}, false, nil
	}

	s.mu.RLock()
	for _, c := range s.containers {
		if c.ServiceKey == serviceKey {
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
	var memoryReservation int64
	var memoryLimit int64
	var createdAt string
	var registeredAt string
	var startedAt string
	var updatedAt string
	var lastEventID sql.NullInt64
	var healthStatus string
	var healthFailingStreak int
	var unhealthySince string
	var restartLoop int
	var restartStreak int
	var restartLoopSince string
	var healthcheck sql.NullString
	var noNewPrivileges int
	var finishedAt sql.NullString
	var exitCode sql.NullInt64

	err := s.db.QueryRowContext(ctx, `SELECT id, name, service_key, compose_service, compose_project, compose_workdir, container_id, current_container_name, image, image_tag, image_id, created_at_container, registered_at, started_at, finished_at, exit_code, status, role, caps, read_only, no_new_privileges, memory_reservation, memory_limit, user, last_event_id, updated_at, present, health_status, health_failing_streak, unhealthy_since, restart_loop, restart_streak, restart_loop_since, healthcheck FROM containers WHERE service_key = ?`, serviceKey).Scan(&c.ID, &c.Name, &c.ServiceKey, &c.ComposeService, &c.ComposeProject, &c.ComposeWorkdir, &c.ContainerID, &c.CurrentContainerName, &c.Image, &c.ImageTag, &c.ImageID, &createdAt, &registeredAt, &startedAt, &finishedAt, &exitCode, &c.Status, &c.Role, &capsJSON, &readOnly, &noNewPrivileges, &memoryReservation, &memoryLimit, &c.User, &lastEventID, &updatedAt, &present, &healthStatus, &healthFailingStreak, &unhealthySince, &restartLoop, &restartStreak, &restartLoopSince, &healthcheck)
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
	c.NoNewPrivileges = noNewPrivileges == 1
	c.MemoryReservation = memoryReservation
	c.MemoryLimit = memoryLimit
	c.CreatedAt = parseTime(createdAt)
	c.RegisteredAt = parseTime(registeredAt)
	c.StartedAt = parseTime(startedAt)
	if finishedAt.Valid {
		c.FinishedAt = parseTime(finishedAt.String)
	}
	if exitCode.Valid {
		val := int(exitCode.Int64)
		c.ExitCode = &val
	}
	c.UpdatedAt = parseTime(updatedAt)
	if lastEventID.Valid {
		c.LastEventID = lastEventID.Int64
	}
	c.Present = present == 1
	c.HealthStatus = healthStatus
	c.HealthFailingStreak = healthFailingStreak
	c.UnhealthySince = parseTime(unhealthySince)
	c.RestartLoop = restartLoop == 1
	c.RestartStreak = restartStreak
	c.RestartLoopSince = parseTime(restartLoopSince)
	if parsed, err := parseHealthcheck(healthcheck); err != nil {
		return Container{}, false, err
	} else {
		c.Healthcheck = parsed
	}
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
	var memoryReservation int64
	var memoryLimit int64
	var createdAt string
	var registeredAt string
	var startedAt string
	var updatedAt string
	var lastEventID sql.NullInt64
	var healthStatus string
	var healthFailingStreak int
	var unhealthySince string
	var restartLoop int
	var restartStreak int
	var restartLoopSince string
	var healthcheck sql.NullString

	var noNewPrivileges int
	var finishedAt sql.NullString
	var exitCode sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT id, name, service_key, compose_service, compose_project, compose_workdir, container_id, current_container_name, image, image_tag, image_id, created_at_container, registered_at, started_at, finished_at, exit_code, status, role, caps, read_only, no_new_privileges, memory_reservation, memory_limit, user, last_event_id, updated_at, present, health_status, health_failing_streak, unhealthy_since, restart_loop, restart_streak, restart_loop_since, healthcheck FROM containers WHERE container_id = ?`, containerID).Scan(&c.ID, &c.Name, &c.ServiceKey, &c.ComposeService, &c.ComposeProject, &c.ComposeWorkdir, &c.ContainerID, &c.CurrentContainerName, &c.Image, &c.ImageTag, &c.ImageID, &createdAt, &registeredAt, &startedAt, &finishedAt, &exitCode, &c.Status, &c.Role, &capsJSON, &readOnly, &noNewPrivileges, &memoryReservation, &memoryLimit, &c.User, &lastEventID, &updatedAt, &present, &healthStatus, &healthFailingStreak, &unhealthySince, &restartLoop, &restartStreak, &restartLoopSince, &healthcheck)
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
	c.NoNewPrivileges = noNewPrivileges == 1
	c.MemoryReservation = memoryReservation
	c.MemoryLimit = memoryLimit
	c.CreatedAt = parseTime(createdAt)
	c.RegisteredAt = parseTime(registeredAt)
	c.StartedAt = parseTime(startedAt)
	if finishedAt.Valid {
		c.FinishedAt = parseTime(finishedAt.String)
	}
	if exitCode.Valid {
		val := int(exitCode.Int64)
		c.ExitCode = &val
	}
	c.UpdatedAt = parseTime(updatedAt)
	if lastEventID.Valid {
		c.LastEventID = lastEventID.Int64
	}
	c.Present = present == 1
	c.HealthStatus = healthStatus
	c.HealthFailingStreak = healthFailingStreak
	c.UnhealthySince = parseTime(unhealthySince)
	c.RestartLoop = restartLoop == 1
	c.RestartStreak = restartStreak
	c.RestartLoopSince = parseTime(restartLoopSince)
	if parsed, err := parseHealthcheck(healthcheck); err != nil {
		return Container{}, false, err
	} else {
		c.Healthcheck = parsed
	}
	if c.Role == "" {
		c.Role = "service"
	}
	s.mu.Lock()
	s.containers[c.Name] = &c
	s.mu.Unlock()
	return c, true, nil
}

func (s *Store) UpsertContainer(ctx context.Context, c Container) error {
	if c.Role == "" {
		c.Role = "service"
	}
	if c.CurrentContainerName == "" {
		c.CurrentContainerName = c.Name
	}
	now := time.Now().UTC()
	if c.RegisteredAt.IsZero() {
		if existing, ok := s.GetContainer(c.Name); ok && !existing.RegisteredAt.IsZero() {
			c.RegisteredAt = existing.RegisteredAt
		} else if !c.CreatedAt.IsZero() && c.CreatedAt.Before(now) {
			c.RegisteredAt = c.CreatedAt
		} else {
			c.RegisteredAt = now
		}
	}
	if c.StartedAt.IsZero() {
		if existing, ok := s.GetContainer(c.Name); ok && !existing.StartedAt.IsZero() {
			c.StartedAt = existing.StartedAt
		}
	}
	if c.LastEventID == 0 {
		if existing, ok := s.GetContainer(c.Name); ok && existing.LastEventID > 0 {
			c.LastEventID = existing.LastEventID
		}
	}
	if !c.Present {
		c.Present = true
	}

	existingByKey, hasByKey, err := s.GetContainerByServiceKey(ctx, c.ServiceKey)
	if err != nil {
		return err
	}
	existingByName, hasByName := s.GetContainer(c.Name)
	existingByID, hasByID, err := s.GetContainerByContainerID(ctx, c.ContainerID)
	if err != nil {
		return err
	}

	if hasByKey {
		if c.RegisteredAt.IsZero() {
			c.RegisteredAt = existingByKey.RegisteredAt
		}
		if c.StartedAt.IsZero() {
			c.StartedAt = existingByKey.StartedAt
		}
		if c.LastEventID == 0 {
			c.LastEventID = existingByKey.LastEventID
		}
	}
	if hasByName {
		if c.RegisteredAt.IsZero() {
			c.RegisteredAt = existingByName.RegisteredAt
		}
		if c.StartedAt.IsZero() {
			c.StartedAt = existingByName.StartedAt
		}
		if c.LastEventID == 0 {
			c.LastEventID = existingByName.LastEventID
		}
	}
	if hasByID {
		if c.RegisteredAt.IsZero() {
			c.RegisteredAt = existingByID.RegisteredAt
		}
		if c.StartedAt.IsZero() {
			c.StartedAt = existingByID.StartedAt
		}
		if c.LastEventID == 0 {
			c.LastEventID = existingByID.LastEventID
		}
	}

	targetID := int64(0)
	oldName := ""
	canonical := Container{}
	hasCanonical := false
	if hasByKey {
		canonical = existingByKey
		hasCanonical = true
	} else if hasByName {
		canonical = existingByName
		hasCanonical = true
	}

	if hasCanonical && hasByID && canonical.ID != existingByID.ID {
		if err := s.mergeContainers(ctx, canonical, existingByID); err != nil {
			return err
		}
		targetID = canonical.ID
		oldName = canonical.Name
	} else if hasCanonical {
		targetID = canonical.ID
		oldName = canonical.Name
	} else if hasByID {
		targetID = existingByID.ID
		oldName = existingByID.Name
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
	restartLoop := 0
	if c.RestartLoop {
		restartLoop = 1
	}
	healthcheckJSON, err := marshalHealthcheck(c.Healthcheck)
	if err != nil {
		return err
	}

	if targetID == 0 {
		if err := s.db.QueryRowContext(ctx, `
INSERT INTO containers (name, service_key, compose_service, compose_project, compose_workdir, container_id, current_container_name, image, image_tag, image_id, created_at_container, first_seen_at, registered_at, started_at, finished_at, exit_code, status, role, caps, read_only, no_new_privileges, memory_reservation, memory_limit, user, last_event_id, updated_at, present, health_status, health_failing_streak, unhealthy_since, restart_loop, restart_streak, restart_loop_since, healthcheck)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id
`, c.Name, c.ServiceKey, c.ComposeService, c.ComposeProject, c.ComposeWorkdir, c.ContainerID, c.CurrentContainerName, c.Image, c.ImageTag, c.ImageID, formatTime(c.CreatedAt), formatTime(c.RegisteredAt), formatTime(c.RegisteredAt), formatTime(c.StartedAt), nullTime(c.FinishedAt), nullIntPtr(c.ExitCode), c.Status, c.Role, string(capsJSON), readOnly, boolToInt(c.NoNewPrivileges), c.MemoryReservation, c.MemoryLimit, c.User, nullInt(c.LastEventID), formatTime(c.UpdatedAt), present, c.HealthStatus, c.HealthFailingStreak, formatTime(c.UnhealthySince), restartLoop, c.RestartStreak, formatTime(c.RestartLoopSince), healthcheckJSON).Scan(&targetID); err != nil {
			return err
		}
	} else {
		if _, err := s.db.ExecContext(ctx, `
UPDATE containers
SET name = ?, service_key = ?, compose_service = ?, compose_project = ?, compose_workdir = ?, container_id = ?,
    current_container_name = ?, image = ?, image_tag = ?, image_id = ?, created_at_container = ?, first_seen_at = ?, registered_at = ?, started_at = ?, finished_at = ?,
    exit_code = ?, status = ?, role = ?, caps = ?, read_only = ?, no_new_privileges = ?,
    memory_reservation = ?, memory_limit = ?, user = ?, last_event_id = ?, updated_at = ?, present = ?,
    health_status = ?, health_failing_streak = ?, unhealthy_since = ?, restart_loop = ?, restart_streak = ?,
    restart_loop_since = ?, healthcheck = ?
WHERE id = ?
`, c.Name, c.ServiceKey, c.ComposeService, c.ComposeProject, c.ComposeWorkdir, c.ContainerID, c.CurrentContainerName, c.Image, c.ImageTag, c.ImageID, formatTime(c.CreatedAt), formatTime(c.RegisteredAt), formatTime(c.RegisteredAt), formatTime(c.StartedAt), nullTime(c.FinishedAt), nullIntPtr(c.ExitCode), c.Status, c.Role, string(capsJSON), readOnly, boolToInt(c.NoNewPrivileges), c.MemoryReservation, c.MemoryLimit, c.User, nullInt(c.LastEventID), formatTime(c.UpdatedAt), present, c.HealthStatus, c.HealthFailingStreak, formatTime(c.UnhealthySince), restartLoop, c.RestartStreak, formatTime(c.RestartLoopSince), healthcheckJSON, targetID); err != nil {
			return err
		}
	}

	copy := c
	copy.ID = targetID
	s.mu.Lock()
	if oldName != "" && oldName != c.Name {
		delete(s.containers, oldName)
	}
	s.containers[c.Name] = &copy
	s.mu.Unlock()
	return nil
}

func (s *Store) mergeContainers(ctx context.Context, keep, remove Container) error {
	if keep.ID == 0 || remove.ID == 0 || keep.ID == remove.ID {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, `
UPDATE events
SET container_pk = ?, container_name = ?
WHERE container_pk = ?
`, keep.ID, keep.Name, remove.ID); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `
UPDATE alerts
SET container_pk = ?, container_name = ?
WHERE container_pk = ?
`, keep.ID, keep.Name, remove.ID); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM containers WHERE id = ?`, remove.ID); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.containers, remove.Name)
	s.mu.Unlock()
	return nil
}

func (s *Store) AddEvent(ctx context.Context, e Event) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
INSERT INTO events (container_pk, container_name, container_id, parsed_container_name, event_type, severity, message, ts, old_image, new_image, old_image_id, new_image_id, reason, details, exit_code)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, e.ContainerPK, e.Container, e.ContainerID, nullStr(e.ParsedContainerName), e.Type, e.Severity, e.Message, formatTime(e.Timestamp), nullStr(e.OldImage), nullStr(e.NewImage), nullStr(e.OldImageID), nullStr(e.NewImageID), nullStr(e.Reason), nullStr(e.DetailsJSON), nullIntPtr(e.ExitCode))
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
SELECT id, container_name, container_id, event_type, severity, message, ts, old_image, new_image, old_image_id, new_image_id, reason, details, container_pk, exit_code
     , parsed_container_name
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
		var exitCode sql.NullInt64
		var parsedContainerName sql.NullString
		if err := rows.Scan(&e.ID, &e.Container, &e.ContainerID, &e.Type, &e.Severity, &e.Message, &ts, &oldImage, &newImage, &oldImageID, &newImageID, &reason, &details, &e.ContainerPK, &exitCode, &parsedContainerName); err != nil {
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
		if exitCode.Valid {
			val := int(exitCode.Int64)
			e.ExitCode = &val
		}
		if parsedContainerName.Valid {
			e.ParsedContainerName = parsedContainerName.String
		}
		e.Container = s.resolveContainerName(e.ContainerPK, e.ContainerID, e.Container)
		items = append(items, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Store) CountEventsByContainer(ctx context.Context, container string) (int64, error) {
	containerInfo, ok, err := s.GetContainerByName(ctx, container)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, nil
	}

	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM events WHERE container_pk = ?`, containerInfo.ID).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func (s *Store) resolveContainerName(containerPK int64, containerID, fallback string) string {
	if containerPK > 0 {
		s.mu.RLock()
		for _, c := range s.containers {
			if c.ID == containerPK {
				name := c.Name
				s.mu.RUnlock()
				return name
			}
		}
		s.mu.RUnlock()
		if c, ok, _ := s.getContainerByPK(context.Background(), containerPK); ok && c.Name != "" {
			return c.Name
		}
	}
	if containerID != "" {
		if c, ok, _ := s.GetContainerByContainerID(context.Background(), containerID); ok && c.Name != "" {
			return c.Name
		}
	}
	return fallback
}

func (s *Store) getContainerByPK(ctx context.Context, containerPK int64) (Container, bool, error) {
	if containerPK <= 0 {
		return Container{}, false, nil
	}

	var c Container
	var capsJSON string
	var readOnly int
	var present int
	var memoryReservation int64
	var memoryLimit int64
	var createdAt string
	var registeredAt string
	var startedAt string
	var updatedAt string
	var lastEventID sql.NullInt64
	var healthStatus string
	var healthFailingStreak int
	var unhealthySince string
	var restartLoop int
	var restartStreak int
	var restartLoopSince string
	var healthcheck sql.NullString
	var noNewPrivileges int
	var finishedAt sql.NullString
	var exitCode sql.NullInt64

	err := s.db.QueryRowContext(ctx, `SELECT id, name, service_key, compose_service, compose_project, compose_workdir, container_id, current_container_name, image, image_tag, image_id, created_at_container, registered_at, started_at, finished_at, exit_code, status, role, caps, read_only, no_new_privileges, memory_reservation, memory_limit, user, last_event_id, updated_at, present, health_status, health_failing_streak, unhealthy_since, restart_loop, restart_streak, restart_loop_since, healthcheck FROM containers WHERE id = ?`, containerPK).Scan(&c.ID, &c.Name, &c.ServiceKey, &c.ComposeService, &c.ComposeProject, &c.ComposeWorkdir, &c.ContainerID, &c.CurrentContainerName, &c.Image, &c.ImageTag, &c.ImageID, &createdAt, &registeredAt, &startedAt, &finishedAt, &exitCode, &c.Status, &c.Role, &capsJSON, &readOnly, &noNewPrivileges, &memoryReservation, &memoryLimit, &c.User, &lastEventID, &updatedAt, &present, &healthStatus, &healthFailingStreak, &unhealthySince, &restartLoop, &restartStreak, &restartLoopSince, &healthcheck)
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
	c.NoNewPrivileges = noNewPrivileges == 1
	c.MemoryReservation = memoryReservation
	c.MemoryLimit = memoryLimit
	c.CreatedAt = parseTime(createdAt)
	c.RegisteredAt = parseTime(registeredAt)
	c.StartedAt = parseTime(startedAt)
	if finishedAt.Valid {
		c.FinishedAt = parseTime(finishedAt.String)
	}
	if exitCode.Valid {
		val := int(exitCode.Int64)
		c.ExitCode = &val
	}
	c.UpdatedAt = parseTime(updatedAt)
	if lastEventID.Valid {
		c.LastEventID = lastEventID.Int64
	}
	c.Present = present == 1
	c.HealthStatus = healthStatus
	c.HealthFailingStreak = healthFailingStreak
	c.UnhealthySince = parseTime(unhealthySince)
	c.RestartLoop = restartLoop == 1
	c.RestartStreak = restartStreak
	c.RestartLoopSince = parseTime(restartLoopSince)
	if parsed, err := parseHealthcheck(healthcheck); err != nil {
		return Container{}, false, err
	} else {
		c.Healthcheck = parsed
	}
	if c.Role == "" {
		c.Role = "service"
	}

	s.mu.Lock()
	s.containers[c.Name] = &c
	s.mu.Unlock()
	return c, true, nil
}

func (s *Store) ListAllEvents(ctx context.Context, beforeID int64, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 50
	}
	if beforeID <= 0 {
		beforeID = int64(^uint64(0) >> 1)
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT id, container_name, container_id, event_type, severity, message, ts, old_image, new_image, old_image_id, new_image_id, reason, details, container_pk, exit_code
     , parsed_container_name
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
		var exitCode sql.NullInt64
		var parsedContainerName sql.NullString
		if err := rows.Scan(&e.ID, &e.Container, &e.ContainerID, &e.Type, &e.Severity, &e.Message, &ts, &oldImage, &newImage, &oldImageID, &newImageID, &reason, &details, &e.ContainerPK, &exitCode, &parsedContainerName); err != nil {
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
		if exitCode.Valid {
			val := int(exitCode.Int64)
			e.ExitCode = &val
		}
		if parsedContainerName.Valid {
			e.ParsedContainerName = parsedContainerName.String
		}
		e.Container = s.resolveContainerName(e.ContainerPK, e.ContainerID, e.Container)
		items = append(items, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Store) CountAllEvents(ctx context.Context) (int64, error) {
	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM events`).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func (s *Store) AddAlert(ctx context.Context, a Alert) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
INSERT INTO alerts (container_pk, container_name, container_id, parsed_container_name, alert_type, severity, message, ts, old_image, new_image, old_image_id, new_image_id, reason, details, exit_code)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, a.ContainerPK, a.Container, a.ContainerID, nullStr(a.ParsedContainerName), a.Type, a.Severity, a.Message, formatTime(a.Timestamp), nullStr(a.OldImage), nullStr(a.NewImage), nullStr(a.OldImageID), nullStr(a.NewImageID), nullStr(a.Reason), nullStr(a.DetailsJSON), nullIntPtr(a.ExitCode))
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, nil
}

func (s *Store) ListAllAlerts(ctx context.Context, beforeID int64, limit int) ([]Alert, error) {
	if limit <= 0 {
		limit = 50
	}
	if beforeID <= 0 {
		beforeID = int64(^uint64(0) >> 1)
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT id, container_name, container_id, alert_type, severity, message, ts, old_image, new_image, old_image_id, new_image_id, reason, details, container_pk, exit_code
     , parsed_container_name
FROM alerts
WHERE id < ?
ORDER BY id DESC
LIMIT ?
`, beforeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []Alert{}
	for rows.Next() {
		var a Alert
		var ts string
		var oldImage, newImage, oldImageID, newImageID, reason, details sql.NullString
		var exitCode sql.NullInt64
		var parsedContainerName sql.NullString
		if err := rows.Scan(&a.ID, &a.Container, &a.ContainerID, &a.Type, &a.Severity, &a.Message, &ts, &oldImage, &newImage, &oldImageID, &newImageID, &reason, &details, &a.ContainerPK, &exitCode, &parsedContainerName); err != nil {
			return nil, err
		}
		a.Timestamp = parseTime(ts)
		if oldImage.Valid {
			a.OldImage = oldImage.String
		}
		if newImage.Valid {
			a.NewImage = newImage.String
		}
		if oldImageID.Valid {
			a.OldImageID = oldImageID.String
		}
		if newImageID.Valid {
			a.NewImageID = newImageID.String
		}
		if reason.Valid {
			a.Reason = reason.String
		}
		if details.Valid {
			a.DetailsJSON = details.String
		}
		if exitCode.Valid {
			val := int(exitCode.Int64)
			a.ExitCode = &val
		}
		if parsedContainerName.Valid {
			a.ParsedContainerName = parsedContainerName.String
		}
		a.Container = s.resolveContainerName(a.ContainerPK, a.ContainerID, a.Container)
		items = append(items, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Store) CountAllAlerts(ctx context.Context) (int64, error) {
	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM alerts`).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func (s *Store) GetEvent(ctx context.Context, id int64) (Event, bool, error) {
	if id <= 0 {
		return Event{}, false, nil
	}
	var e Event
	var ts string
	var oldImage, newImage, oldImageID, newImageID, reason, details sql.NullString
	var exitCode sql.NullInt64
	var parsedContainerName sql.NullString
	err := s.db.QueryRowContext(ctx, `
SELECT id, container_name, container_id, event_type, severity, message, ts, old_image, new_image, old_image_id, new_image_id, reason, details, container_pk, exit_code
     , parsed_container_name
FROM events
WHERE id = ?
`, id).Scan(&e.ID, &e.Container, &e.ContainerID, &e.Type, &e.Severity, &e.Message, &ts, &oldImage, &newImage, &oldImageID, &newImageID, &reason, &details, &e.ContainerPK, &exitCode, &parsedContainerName)
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
	if exitCode.Valid {
		val := int(exitCode.Int64)
		e.ExitCode = &val
	}
	if parsedContainerName.Valid {
		e.ParsedContainerName = parsedContainerName.String
	}
	e.Container = s.resolveContainerName(e.ContainerPK, e.ContainerID, e.Container)
	return e, true, nil
}

func (s *Store) GetLatestEventByContainerPK(ctx context.Context, containerPK int64) (Event, bool, error) {
	if containerPK == 0 {
		return Event{}, false, nil
	}
	var e Event
	var ts string
	var oldImage, newImage, oldImageID, newImageID, reason, details sql.NullString
	var exitCode sql.NullInt64
	var parsedContainerName sql.NullString
	err := s.db.QueryRowContext(ctx, `
SELECT id, container_name, container_id, event_type, severity, message, ts, old_image, new_image, old_image_id, new_image_id, reason, details, container_pk, exit_code
     , parsed_container_name
FROM events
WHERE container_pk = ?
ORDER BY ts DESC
LIMIT 1
`, containerPK).Scan(&e.ID, &e.Container, &e.ContainerID, &e.Type, &e.Severity, &e.Message, &ts, &oldImage, &newImage, &oldImageID, &newImageID, &reason, &details, &e.ContainerPK, &exitCode, &parsedContainerName)
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
	if exitCode.Valid {
		val := int(exitCode.Int64)
		e.ExitCode = &val
	}
	if parsedContainerName.Valid {
		e.ParsedContainerName = parsedContainerName.String
	}
	e.Container = s.resolveContainerName(e.ContainerPK, e.ContainerID, e.Container)
	return e, true, nil
}

func (s *Store) GetLatestRestartTimestampByContainerPK(ctx context.Context, containerPK int64) (time.Time, bool, error) {
	var ts string
	err := s.db.QueryRowContext(ctx, `
SELECT ts
FROM events
WHERE container_pk = ? AND event_type = 'restart'
ORDER BY id DESC
LIMIT 1
`, containerPK).Scan(&ts)
	if err == sql.ErrNoRows {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	return parseTime(ts), true, nil
}

func (s *Store) GetLatestRestartLoopAlertByContainerPK(ctx context.Context, containerPK int64) (Alert, bool, error) {
	var a Alert
	var ts string
	var oldImage, newImage, oldImageID, newImageID, reason, details sql.NullString
	var exitCode sql.NullInt64
	var parsedContainerName sql.NullString
	err := s.db.QueryRowContext(ctx, `
SELECT id, container_name, container_id, alert_type, severity, message, ts, old_image, new_image, old_image_id, new_image_id, reason, details, container_pk, exit_code
     , parsed_container_name
FROM alerts
WHERE container_pk = ? AND alert_type IN ('restart_loop', 'restart_healed')
ORDER BY id DESC
LIMIT 1
`, containerPK).Scan(&a.ID, &a.Container, &a.ContainerID, &a.Type, &a.Severity, &a.Message, &ts, &oldImage, &newImage, &oldImageID, &newImageID, &reason, &details, &a.ContainerPK, &exitCode, &parsedContainerName)
	if err == sql.ErrNoRows {
		return Alert{}, false, nil
	}
	if err != nil {
		return Alert{}, false, err
	}
	a.Timestamp = parseTime(ts)
	if oldImage.Valid {
		a.OldImage = oldImage.String
	}
	if newImage.Valid {
		a.NewImage = newImage.String
	}
	if oldImageID.Valid {
		a.OldImageID = oldImageID.String
	}
	if newImageID.Valid {
		a.NewImageID = newImageID.String
	}
	if reason.Valid {
		a.Reason = reason.String
	}
	if details.Valid {
		a.DetailsJSON = details.String
	}
	if exitCode.Valid {
		val := int(exitCode.Int64)
		a.ExitCode = &val
	}
	if parsedContainerName.Valid {
		a.ParsedContainerName = parsedContainerName.String
	}
	a.Container = s.resolveContainerName(a.ContainerPK, a.ContainerID, a.Container)
	return a, true, nil
}

func (s *Store) GetLatestEventTimestamp(ctx context.Context) (time.Time, bool, error) {
	var ts sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT MAX(ts) FROM events`).Scan(&ts); err != nil {
		return time.Time{}, false, err
	}
	if !ts.Valid || strings.TrimSpace(ts.String) == "" {
		return time.Time{}, false, nil
	}
	return parseTime(ts.String), true, nil
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
	if newName == "" {
		return nil
	}
	if info.Name == "" {
		if byID, _, ok := s.FindContainerByID(info.ContainerID); ok {
			info.Name = byID.Name
		} else {
			info.Name = oldName
		}
	}
	if info.Name == "" {
		return nil
	}
	if existing, ok := s.GetContainer(info.Name); ok {
		if info.RegisteredAt.IsZero() {
			info.RegisteredAt = existing.RegisteredAt
		}
		if info.StartedAt.IsZero() {
			info.StartedAt = existing.StartedAt
		}
		if info.LastEventID == 0 {
			info.LastEventID = existing.LastEventID
		}
		if info.Role == "" {
			info.Role = existing.Role
		}
	}
	info.CurrentContainerName = newName
	info.Present = true
	return s.UpsertContainer(ctx, info)
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

func nullIntPtr(val *int) interface{} {
	if val == nil {
		return nil
	}
	return *val
}

func nullInt(val int64) interface{} {
	if val == 0 {
		return nil
	}
	return val
}

func nullTime(t time.Time) interface{} {
	if t.IsZero() {
		return nil
	}
	return formatTime(t)
}

func marshalHealthcheck(val *Healthcheck) (string, error) {
	if val == nil {
		return "", nil
	}
	raw, err := json.Marshal(val)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func mustHealthcheck(val *Healthcheck) string {
	raw, err := marshalHealthcheck(val)
	if err != nil {
		return ""
	}
	return raw
}

func parseHealthcheck(val sql.NullString) (*Healthcheck, error) {
	if !val.Valid {
		return nil, nil
	}
	trimmed := strings.TrimSpace(val.String)
	if trimmed == "" {
		return nil, nil
	}
	var out Healthcheck
	if err := json.Unmarshal([]byte(trimmed), &out); err != nil {
		return nil, err
	}
	return &out, nil
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
