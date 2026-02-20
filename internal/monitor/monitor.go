package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"healthmon/internal/api"
	"healthmon/internal/config"
	"healthmon/internal/notify"
	"healthmon/internal/store"

	"github.com/distribution/reference"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/client"
)

type Monitor struct {
	cfg        config.Config
	store      *store.Store
	server     *api.Server
	telegram   *notify.Telegram
	restarts   *restartTracker
	docker     *client.Client
	capDefault []string
}

func New(cfg config.Config, store *store.Store, server *api.Server) *Monitor {
	return &Monitor{
		cfg:        cfg,
		store:      store,
		server:     server,
		telegram:   notify.NewTelegram(cfg.TelegramEnabled, cfg.TelegramToken, cfg.TelegramChatID),
		restarts:   newRestartTracker(cfg.RestartWindowSeconds, cfg.RestartThreshold),
		capDefault: defaultCaps(),
	}
}

func (m *Monitor) Start(ctx context.Context) error {
	cli, err := client.NewClientWithOpts(client.WithHost(m.cfg.DockerHost), client.WithAPIVersionNegotiation())
	if err != nil {
		return err
	}
	m.docker = cli

	if err := m.syncExisting(ctx); err != nil {
		return err
	}

	go m.watchHeals(ctx)

	stream := cli.Events(ctx, client.EventsListOptions{})
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-stream.Err:
			return err
		case msg := <-stream.Messages:
			if msg.Type != "container" {
				continue
			}
			m.handleEvent(ctx, msg)
		}
	}
}

func (m *Monitor) syncExisting(ctx context.Context) error {
	result, err := m.docker.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return err
	}

	presentNames := make(map[string]struct{}, len(result.Items))
	for _, c := range result.Items {
		if len(c.Names) == 0 {
			continue
		}
		name := strings.TrimPrefix(c.Names[0], "/")
		presentNames[name] = struct{}{}
		inspect, err := m.docker.ContainerInspect(ctx, c.ID, client.ContainerInspectOptions{})
		if err != nil {
			continue
		}
		info := m.inspectToContainer(inspect.Container)
		info.Name = name
		autoRestart := hasAutoRestartPolicy(inspect.Container)
		now := time.Now().UTC()
		if existing, ok := m.store.GetContainer(name); ok {
			info.RegisteredAt = existing.RegisteredAt
			if info.StartedAt.IsZero() {
				info.StartedAt = existing.StartedAt
			}
			info.UnhealthySince = existing.UnhealthySince
			if strings.ToLower(info.HealthStatus) == "unhealthy" && info.UnhealthySince.IsZero() {
				info.UnhealthySince = now
			}
			if strings.ToLower(info.HealthStatus) != "unhealthy" {
				info.UnhealthySince = time.Time{}
			}
			if autoRestart {
				info.RestartLoop = existing.RestartLoop
				info.RestartStreak = existing.RestartStreak
				info.RestartLoopSince = existing.RestartLoopSince
				if latestLoopAlert, found, err := m.store.GetLatestRestartLoopAlertByContainerPK(ctx, existing.ID); err == nil && found {
					if latestLoopAlert.Type == "restart_loop" {
						info.RestartLoop = true
						if count := parseRestartCount(latestLoopAlert.DetailsJSON); count > 0 {
							info.RestartStreak = count
						}
						if !latestLoopAlert.Timestamp.IsZero() {
							info.RestartLoopSince = latestLoopAlert.Timestamp
						}
					} else if latestLoopAlert.Type == "restart_healed" {
						info.RestartLoop = false
						info.RestartStreak = 0
						info.RestartLoopSince = time.Time{}
					}
				}
				// If monitor was down and container has been running longer than the
				// restart-loop window, treat loop as healed on startup sync.
				if info.RestartLoop && strings.ToLower(info.Status) == "running" && !info.StartedAt.IsZero() && now.Sub(info.StartedAt) > m.restarts.window {
					info.RestartLoop = false
					info.RestartStreak = 0
					info.RestartLoopSince = time.Time{}
					m.restarts.markHealed(name)
				}
			} else {
				info.RestartLoop = false
				info.RestartStreak = 0
				info.RestartLoopSince = time.Time{}
				m.restarts.reset(name)
			}
		}
		if strings.ToLower(info.HealthStatus) == "unhealthy" && info.UnhealthySince.IsZero() {
			info.UnhealthySince = now
		}
		if info.RegisteredAt.IsZero() {
			info.RegisteredAt = minTime(info.CreatedAt, now)
		}
		if err := m.store.UpsertContainer(ctx, info); err != nil {
			return err
		}
	}
	if err := m.store.MarkAbsentExcept(ctx, presentNames); err != nil {
		return err
	}
	return nil
}

func (m *Monitor) handleEvent(ctx context.Context, msg events.Message) {
	name := msg.Actor.Attributes["name"]
	if name == "" && msg.Actor.ID != "" {
		if container, foundName, ok := m.store.FindContainerByID(msg.Actor.ID); ok {
			_ = container
			name = foundName
		}
	}
	if name == "" {
		return
	}
	name = strings.TrimPrefix(name, "/")
	if isHealthcheckExecEvent(msg) {
		return
	}
	if !isHealthcheckStatusEvent(msg) {
		log.Printf("event: container=%s action=%s id=%s", name, msg.Action, msg.Actor.ID)
	}

	switch {
	case msg.Action == "create":
		m.handleCreate(ctx, name, msg.Actor.ID)
	case msg.Action == "start":
		m.handleStart(ctx, name, msg.Actor.ID)
	case msg.Action == "stop":
		exitCode := parseExitCode(msg.Actor.Attributes["exitCode"])
		m.handleStop(ctx, name, msg.Actor.ID, exitCode)
	case msg.Action == "die":
		exitCode := parseExitCode(msg.Actor.Attributes["exitCode"])
		if exitCode == nil || *exitCode == 0 {
			m.handleStop(ctx, name, msg.Actor.ID, exitCode)
		} else {
			m.handleRestartLike(ctx, name, msg.Actor.ID, "die", exitCode, "")
		}
	case msg.Action == "restart":
		m.handleRestartLike(ctx, name, msg.Actor.ID, "restart", nil, "")
	case msg.Action == "oom":
		m.handleRestartLike(ctx, name, msg.Actor.ID, "oom", nil, "")
	case msg.Action == "kill":
		m.handleSignal(ctx, name, msg.Actor.ID, strings.TrimSpace(msg.Actor.Attributes["signal"]))
	case strings.HasPrefix(string(msg.Action), "health_status:"):
		m.handleHealth(ctx, name, msg.Actor.ID, strings.TrimSpace(strings.TrimPrefix(string(msg.Action), "health_status:")))
	case msg.Action == "rename":
		m.handleRename(ctx, msg, name)
	case msg.Action == "destroy" || msg.Action == "remove" || msg.Action == "rm":
		_ = m.store.SetContainerPresent(ctx, name, false)
		m.server.Broadcast(ctx, api.EventUpdate{Container: api.ContainerResponse{Name: name, Present: false}})
	}
}

func (m *Monitor) handleCreate(ctx context.Context, name, id string) {
	inspect, err := m.docker.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if err != nil {
		return
	}

	newInfo := m.inspectToContainer(inspect.Container)
	newInfo.Name = name

	now := time.Now().UTC()
	existing, has := m.store.GetContainer(name)
	if has {
		newInfo.RegisteredAt = existing.RegisteredAt
		if newInfo.StartedAt.IsZero() {
			newInfo.StartedAt = existing.StartedAt
		}
		newInfo.UnhealthySince = existing.UnhealthySince
		if strings.ToLower(newInfo.HealthStatus) != "unhealthy" {
			newInfo.UnhealthySince = time.Time{}
		}
		newInfo.RestartLoopSince = existing.RestartLoopSince
	} else {
		newInfo.RegisteredAt = minTime(newInfo.CreatedAt, now)
	}
	if strings.ToLower(newInfo.HealthStatus) == "unhealthy" && newInfo.UnhealthySince.IsZero() {
		newInfo.UnhealthySince = now
	}

	if has && existing.ContainerID != id {
		m.restarts.reset(name)
		if existing.RestartLoop {
			newInfo.RestartLoop = true
			newInfo.RestartStreak = existing.RestartStreak
			newInfo.RestartLoopSince = existing.RestartLoopSince
		} else {
			newInfo.RestartLoop = false
			newInfo.RestartStreak = 0
			newInfo.RestartLoopSince = time.Time{}
		}
		imageChanged := existing.ImageID != newInfo.ImageID || existing.ImageTag != newInfo.ImageTag
		if imageChanged {
			m.emitInfo(ctx, name, id, "image_changed", fmt.Sprintf("Image changed %s -> %s", existing.Image, newInfo.Image), existing.Image, newInfo.Image, existing.ImageID, newInfo.ImageID, "recreate", nil)
			m.emitAlert(ctx, name, id, "image_changed", "Container image updated", "blue", nil)
		} else {
			m.emitInfo(ctx, name, id, "recreated", "Container recreated", existing.Image, newInfo.Image, existing.ImageID, newInfo.ImageID, "recreate", nil)
		}
		m.emitAlert(ctx, name, id, "recreated", "Container recreated", "blue", nil)
	}

	_ = m.store.UpsertContainer(ctx, newInfo)
	m.emitInfo(ctx, name, id, "created", "Container created", "", "", "", "", "create", nil)
}

func (m *Monitor) handleStart(ctx context.Context, name, id string) {
	inspect, err := m.docker.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if err != nil {
		return
	}
	info := m.inspectToContainer(inspect.Container)
	info.Name = name
	autoRestart := hasAutoRestartPolicy(inspect.Container)
	if !autoRestart {
		info.RestartLoop = false
		info.RestartStreak = 0
		info.RestartLoopSince = time.Time{}
		m.restarts.reset(name)
	}
	if existing, ok := m.store.GetContainer(name); ok {
		info.RegisteredAt = existing.RegisteredAt
		if info.StartedAt.IsZero() {
			info.StartedAt = existing.StartedAt
		}
		info.UnhealthySince = existing.UnhealthySince
		if strings.ToLower(info.HealthStatus) != "unhealthy" {
			info.UnhealthySince = time.Time{}
		}
		if autoRestart {
			info.RestartLoop = existing.RestartLoop
			info.RestartStreak = existing.RestartStreak
			info.RestartLoopSince = existing.RestartLoopSince
		}
	}
	if strings.ToLower(info.HealthStatus) == "unhealthy" && info.UnhealthySince.IsZero() {
		info.UnhealthySince = time.Now().UTC()
	}
	if info.RegisteredAt.IsZero() {
		info.RegisteredAt = minTime(info.CreatedAt, time.Now().UTC())
	}
	if info.StartedAt.IsZero() {
		info.StartedAt = time.Now().UTC()
	}
	_ = m.store.UpsertContainer(ctx, info)
	m.emitInfo(ctx, name, id, "started", "Container started", "", "", "", "", "start", nil)
}

func (m *Monitor) handleRename(ctx context.Context, msg events.Message, newName string) {
	oldName := msg.Actor.Attributes["oldName"]
	if oldName == "" {
		oldName = msg.Actor.Attributes["old_name"]
	}
	if oldName == "" || newName == "" {
		return
	}
	oldName = strings.TrimPrefix(oldName, "/")
	newName = strings.TrimPrefix(newName, "/")
	target, hasTarget := m.store.GetContainer(newName)

	inspect, err := m.docker.ContainerInspect(ctx, msg.Actor.ID, client.ContainerInspectOptions{})
	if err != nil {
		return
	}
	info := m.inspectToContainer(inspect.Container)
	info.Name = newName
	if existing, ok := m.store.GetContainer(oldName); ok {
		info.RegisteredAt = existing.RegisteredAt
		info.StartedAt = existing.StartedAt
		info.LastEventID = existing.LastEventID
	}
	// If this rename replaces an older logical container name that was still broken,
	// preserve the derived bad state until heal timeout marks it recovered.
	if hasTarget {
		if target.RestartLoop {
			info.RestartLoop = true
			info.RestartLoopSince = target.RestartLoopSince
			if info.RestartStreak < target.RestartStreak {
				info.RestartStreak = target.RestartStreak
			}
		}
		if strings.ToLower(target.HealthStatus) == "unhealthy" {
			info.HealthStatus = target.HealthStatus
			info.HealthFailingStreak = maxInt(info.HealthFailingStreak, target.HealthFailingStreak)
			info.UnhealthySince = target.UnhealthySince
		}
	}
	if info.RegisteredAt.IsZero() {
		info.RegisteredAt = minTime(info.CreatedAt, time.Now().UTC())
	}
	m.restarts.reset(oldName)
	m.restarts.reset(newName)
	_ = m.store.RenameContainer(ctx, oldName, newName, info)
	m.emitInfo(ctx, newName, msg.Actor.ID, "renamed", fmt.Sprintf("Container renamed %s -> %s", oldName, newName), "", "", "", "", "rename", nil)
}

func (m *Monitor) handleHealth(ctx context.Context, name, id, status string) {
	status = strings.TrimSpace(strings.ToLower(status))
	existing, has := m.store.GetContainer(name)
	prevStatus := ""
	prevStreak := 0
	if has {
		prevStatus = strings.ToLower(existing.HealthStatus)
		prevStreak = existing.HealthFailingStreak
	}

	if inspect, err := m.docker.ContainerInspect(ctx, id, client.ContainerInspectOptions{}); err == nil {
		info := m.inspectToContainer(inspect.Container)
		info.Name = name
		if has {
			info.RegisteredAt = existing.RegisteredAt
			info.StartedAt = existing.StartedAt
			info.UnhealthySince = existing.UnhealthySince
			info.RestartLoop = existing.RestartLoop
			info.RestartStreak = existing.RestartStreak
			info.RestartLoopSince = existing.RestartLoopSince
		}
		if strings.ToLower(info.HealthStatus) == "unhealthy" {
			if info.UnhealthySince.IsZero() {
				info.UnhealthySince = time.Now().UTC()
			}
		} else {
			info.UnhealthySince = time.Time{}
		}
		if info.RegisteredAt.IsZero() {
			info.RegisteredAt = minTime(info.CreatedAt, time.Now().UTC())
		}
		_ = m.store.UpsertContainer(ctx, info)
		status = strings.ToLower(info.HealthStatus)
		prevStreak = maxInt(prevStreak, info.HealthFailingStreak)
	} else if has {
		existing.HealthStatus = status
		if status == "unhealthy" {
			existing.HealthFailingStreak = prevStreak + 1
			if existing.UnhealthySince.IsZero() {
				existing.UnhealthySince = time.Now().UTC()
			}
		} else if status == "healthy" {
			existing.HealthFailingStreak = 0
			existing.UnhealthySince = time.Time{}
		}
		existing.UpdatedAt = time.Now().UTC()
		_ = m.store.UpsertContainer(ctx, existing)
	}

	switch status {
	case "unhealthy":
		if prevStatus != "unhealthy" {
			m.emitAlert(ctx, name, id, "unhealthy", "Container became unhealthy", "red", nil)
		}
	case "healthy":
		if prevStatus == "unhealthy" || prevStreak > 0 {
			message := "Container became healthy"
			if prevStreak > 0 {
				message = fmt.Sprintf("Container became healthy after %d failed checks", prevStreak)
			}
			m.emitAlert(ctx, name, id, "healthy", message, "green", nil)
		}
	}
}

func (m *Monitor) handleRestartLike(ctx context.Context, name, id, reason string, exitCode *int, signal string) {
	now := time.Now().UTC()

	inspect, inspectErr := m.docker.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	hasAutoRestart := inspectErr == nil && hasAutoRestartPolicy(inspect.Container)
	wasInLoop := false
	if existing, ok := m.store.GetContainer(name); ok {
		wasInLoop = existing.RestartLoop
	}
	if !hasAutoRestart {
		m.restarts.reset(name)
	}

	streak := 0
	enteredLoop := false
	if hasAutoRestart {
		streak, enteredLoop = m.restarts.record(name, now)
	}
	inLoop := hasAutoRestart && (m.restarts.inLoop(name) || wasInLoop)
	message := fmt.Sprintf("Restart event: %s", reason)
	if signal != "" {
		message = fmt.Sprintf("Restart event: %s (signal %s)", reason, signal)
	}
	m.emitInfo(ctx, name, id, "restart", message, "", "", "", "", reason, exitCode)

	if c, ok := m.store.GetContainer(name); ok {
		c.RestartLoop = inLoop
		if c.RestartLoop {
			if c.RestartStreak <= 0 || enteredLoop {
				c.RestartStreak = streak
			} else {
				c.RestartStreak++
			}
		} else {
			c.RestartStreak = streak
		}
		if c.RestartLoop {
			if c.RestartLoopSince.IsZero() {
				c.RestartLoopSince = now
			}
		} else {
			c.RestartLoopSince = time.Time{}
		}
		c.UpdatedAt = now
		_ = m.store.UpsertContainer(ctx, c)
	}

	if reason == "oom" {
		m.emitAlert(ctx, name, id, "oom_killed", "Container killed by OOM", "red", exitCode)
	}
	if enteredLoop && !wasInLoop {
		details, _ := json.Marshal(map[string]int{"restart_count": streak})
		m.emitAlertRecord(ctx, store.Alert{
			Container:   name,
			ContainerID: id,
			Type:        "restart_loop",
			Severity:    "red",
			Message:     "Restart loop detected",
			Timestamp:   now,
			DetailsJSON: string(details),
		})
	}

	if inspectErr == nil {
		info := m.inspectToContainer(inspect.Container)
		info.Name = name
		computedLoopStreak := streak
		if existing, ok := m.store.GetContainer(name); ok {
			info.RegisteredAt = existing.RegisteredAt
			info.StartedAt = existing.StartedAt
			info.UnhealthySince = existing.UnhealthySince
			if strings.ToLower(info.HealthStatus) != "unhealthy" {
				info.UnhealthySince = time.Time{}
			}
			if strings.ToLower(info.HealthStatus) == "unhealthy" && info.UnhealthySince.IsZero() {
				info.UnhealthySince = now
			}
			info.RestartLoopSince = existing.RestartLoopSince
			computedLoopStreak = existing.RestartStreak
		}
		info.RestartLoop = inLoop
		info.RestartStreak = computedLoopStreak
		if info.RestartLoop {
			if info.RestartLoopSince.IsZero() {
				info.RestartLoopSince = now
			}
		} else {
			info.RestartLoopSince = time.Time{}
		}
		if info.RegisteredAt.IsZero() {
			info.RegisteredAt = minTime(info.CreatedAt, now)
		}
		if info.StartedAt.IsZero() {
			info.StartedAt = now
		}
		_ = m.store.UpsertContainer(ctx, info)
		if shouldAlertNoRestartPolicyFailure(reason, exitCode, inspect.Container) {
			m.emitAlert(ctx, name, id, "failure_no_restart", "Container failed without restart policy", "red", exitCode)
		}
		return
	}

	if existing, ok := m.store.GetContainer(name); ok {
		existing.Status = "exited"
		existing.UpdatedAt = now
		if existing.RegisteredAt.IsZero() {
			existing.RegisteredAt = minTime(existing.CreatedAt, now)
		}
		_ = m.store.UpsertContainer(ctx, existing)
	}
}

func (m *Monitor) handleStop(ctx context.Context, name, id string, exitCode *int) {
	now := time.Now().UTC()
	m.emitInfo(ctx, name, id, "stopped", "Container stopped", "", "", "", "", "stop", exitCode)

	inspect, err := m.docker.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if err == nil {
		info := m.inspectToContainer(inspect.Container)
		info.Name = name
		if existing, ok := m.store.GetContainer(name); ok {
			info.RegisteredAt = existing.RegisteredAt
			info.StartedAt = existing.StartedAt
			info.UnhealthySince = existing.UnhealthySince
			info.RestartLoop = existing.RestartLoop
			info.RestartStreak = existing.RestartStreak
			info.RestartLoopSince = existing.RestartLoopSince
		}
		if strings.ToLower(info.HealthStatus) != "unhealthy" {
			info.UnhealthySince = time.Time{}
		}
		if info.RegisteredAt.IsZero() {
			info.RegisteredAt = minTime(info.CreatedAt, now)
		}
		if info.StartedAt.IsZero() {
			info.StartedAt = now
		}
		_ = m.store.UpsertContainer(ctx, info)
		if shouldAlertNoRestartPolicyFailure("stop", exitCode, inspect.Container) {
			m.emitAlert(ctx, name, id, "failure_no_restart", "Container failed without restart policy", "red", exitCode)
		}
		return
	}

	if existing, ok := m.store.GetContainer(name); ok {
		existing.Status = "exited"
		existing.UpdatedAt = now
		if existing.RegisteredAt.IsZero() {
			existing.RegisteredAt = minTime(existing.CreatedAt, now)
		}
		_ = m.store.UpsertContainer(ctx, existing)
	}
}

func (m *Monitor) handleSignal(ctx context.Context, name, id, signal string) {
	message := "Signal sent"
	reason := "signal"
	if signal != "" {
		message = fmt.Sprintf("Signal sent: %s", signal)
		reason = fmt.Sprintf("signal_%s", strings.ToLower(signal))
	}
	m.emitInfo(ctx, name, id, "signal", message, "", "", "", "", reason, nil)
}

func (m *Monitor) watchHeals(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.checkHeals(ctx)
		}
	}
}

func (m *Monitor) checkHeals(ctx context.Context) {
	now := time.Now().UTC()
	for _, c := range m.store.ListContainers() {
		if !c.RestartLoop {
			continue
		}
		if strings.ToLower(c.Status) != "running" {
			continue
		}

		lastRestart, ok, err := m.store.GetLatestRestartTimestampByContainerPK(ctx, c.ID)
		if err != nil {
			log.Printf("restart heal check failed for %s: %v", c.Name, err)
			continue
		}
		if ok && now.Sub(lastRestart) <= m.restarts.window {
			continue
		}

		streak := c.RestartStreak
		c.RestartLoop = false
		c.RestartStreak = 0
		c.RestartLoopSince = time.Time{}
		c.UpdatedAt = now
		_ = m.store.UpsertContainer(ctx, c)
		m.restarts.markHealed(c.Name)
		message := "Restart loop healed"
		if streak > 0 {
			message = fmt.Sprintf("Restart loop healed after %d restarts", streak)
		}
		details, _ := json.Marshal(map[string]int{"restart_count": streak})
		m.emitAlertRecord(ctx, store.Alert{
			Container:   c.Name,
			ContainerID: c.ContainerID,
			Type:        "restart_healed",
			Severity:    "green",
			Message:     message,
			Timestamp:   now,
			DetailsJSON: string(details),
		})
	}
}

func (m *Monitor) emitInfo(ctx context.Context, name, id, eventType, message, oldImage, newImage, oldImageID, newImageID, reason string, exitCode *int) {
	m.emitEvent(ctx, store.Event{
		Container:   name,
		ContainerID: id,
		Type:        eventType,
		Severity:    "blue",
		Message:     message,
		Timestamp:   time.Now().UTC(),
		OldImage:    oldImage,
		NewImage:    newImage,
		OldImageID:  oldImageID,
		NewImageID:  newImageID,
		Reason:      reason,
		ExitCode:    exitCode,
	})
}

func (m *Monitor) emitAlert(ctx context.Context, name, id, alertType, message, severity string, exitCode *int) {
	alert := store.Alert{
		Container:   name,
		ContainerID: id,
		Type:        alertType,
		Severity:    severity,
		Message:     message,
		Timestamp:   time.Now().UTC(),
		ExitCode:    exitCode,
	}
	m.emitAlertRecord(ctx, alert)
}

func (m *Monitor) emitEvent(ctx context.Context, e store.Event) {
	var container store.Container
	var ok bool
	if e.ContainerID != "" {
		container, ok, _ = m.store.GetContainerByContainerID(ctx, e.ContainerID)
	}
	if !ok {
		container, ok = m.store.GetContainer(e.Container)
	}
	if !ok {
		return
	}

	e.Container = container.Name
	e.ContainerPK = container.ID
	log.Printf("event: type=%s severity=%s container=%s", e.Type, e.Severity, e.Container)
	id, err := m.store.AddEvent(ctx, e)
	if err != nil {
		log.Printf("event persist failed: %v", err)
		return
	}
	e.ID = id
	if latest, latestOK := m.store.GetContainer(container.Name); latestOK {
		container = latest
	}

	eventTotal, err := m.store.CountAllEvents(ctx)
	hasEventTotal := err == nil
	if err != nil {
		log.Printf("event total count failed: %v", err)
	}
	containerEventTotal, err := m.store.CountEventsByContainer(ctx, container.Name)
	hasContainerEventTotal := err == nil
	if err != nil {
		log.Printf("container event total count failed: %v", err)
	}

	update := api.EventUpdate{
		Container: api.ContainerResponse{
			ID:                  container.ID,
			Name:                container.Name,
			ContainerID:         container.ContainerID,
			Image:               container.Image,
			ImageTag:            container.ImageTag,
			ImageID:             container.ImageID,
			CreatedAt:           container.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
			RegisteredAt:        container.RegisteredAt.UTC().Format("2006-01-02T15:04:05Z"),
			StartedAt:           container.StartedAt.UTC().Format("2006-01-02T15:04:05Z"),
			Status:              container.Status,
			Role:                container.Role,
			Caps:                container.Caps,
			ReadOnly:            container.ReadOnly,
			User:                container.User,
			Present:             container.Present,
			HealthStatus:        container.HealthStatus,
			HealthFailingStreak: container.HealthFailingStreak,
			UnhealthySince:      container.UnhealthySince.UTC().Format("2006-01-02T15:04:05Z"),
			RestartLoop:         container.RestartLoop,
			RestartStreak:       container.RestartStreak,
			RestartLoopSince:    container.RestartLoopSince.UTC().Format("2006-01-02T15:04:05Z"),
			Healthcheck:         container.Healthcheck,
		},
		Event: &api.EventResponse{
			ID:          e.ID,
			ContainerPK: container.ID,
			Container:   e.Container,
			ContainerID: e.ContainerID,
			Type:        e.Type,
			Message:     e.Message,
			Timestamp:   e.Timestamp.UTC().Format("2006-01-02T15:04:05Z"),
			OldImage:    e.OldImage,
			NewImage:    e.NewImage,
			OldImageID:  e.OldImageID,
			NewImageID:  e.NewImageID,
			Reason:      e.Reason,
			DetailsJSON: e.DetailsJSON,
			ExitCode:    e.ExitCode,
		},
	}
	if hasEventTotal {
		update.EventTotal = &eventTotal
	}
	if hasContainerEventTotal {
		update.ContainerEventTotal = &containerEventTotal
	}

	m.server.Broadcast(ctx, update)
}

func (m *Monitor) emitAlertRecord(ctx context.Context, a store.Alert) {
	var container store.Container
	var ok bool
	if a.ContainerID != "" {
		container, ok, _ = m.store.GetContainerByContainerID(ctx, a.ContainerID)
	}
	if !ok {
		container, ok = m.store.GetContainer(a.Container)
	}
	if !ok {
		return
	}

	a.Container = container.Name
	a.ContainerPK = container.ID
	log.Printf("alert: type=%s severity=%s container=%s", a.Type, a.Severity, a.Container)
	id, err := m.store.AddAlert(ctx, a)
	if err != nil {
		log.Printf("alert persist failed: %v", err)
		return
	}
	a.ID = id
	if latest, latestOK := m.store.GetContainer(container.Name); latestOK {
		container = latest
	}

	alertTotal, err := m.store.CountAllAlerts(ctx)
	hasAlertTotal := err == nil
	if err != nil {
		log.Printf("alert total count failed: %v", err)
	}

	update := api.EventUpdate{
		Container: api.ContainerResponse{
			ID:                  container.ID,
			Name:                container.Name,
			ContainerID:         container.ContainerID,
			Image:               container.Image,
			ImageTag:            container.ImageTag,
			ImageID:             container.ImageID,
			CreatedAt:           container.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
			RegisteredAt:        container.RegisteredAt.UTC().Format("2006-01-02T15:04:05Z"),
			StartedAt:           container.StartedAt.UTC().Format("2006-01-02T15:04:05Z"),
			Status:              container.Status,
			Role:                container.Role,
			Caps:                container.Caps,
			ReadOnly:            container.ReadOnly,
			User:                container.User,
			Present:             container.Present,
			HealthStatus:        container.HealthStatus,
			HealthFailingStreak: container.HealthFailingStreak,
			UnhealthySince:      container.UnhealthySince.UTC().Format("2006-01-02T15:04:05Z"),
			RestartLoop:         container.RestartLoop,
			RestartStreak:       container.RestartStreak,
			RestartLoopSince:    container.RestartLoopSince.UTC().Format("2006-01-02T15:04:05Z"),
			Healthcheck:         container.Healthcheck,
		},
		Alert: &api.AlertResponse{
			ID:          a.ID,
			ContainerPK: container.ID,
			Container:   a.Container,
			ContainerID: a.ContainerID,
			Type:        a.Type,
			Message:     a.Message,
			Timestamp:   a.Timestamp.UTC().Format("2006-01-02T15:04:05Z"),
			OldImage:    a.OldImage,
			NewImage:    a.NewImage,
			OldImageID:  a.OldImageID,
			NewImageID:  a.NewImageID,
			Reason:      a.Reason,
			DetailsJSON: a.DetailsJSON,
			ExitCode:    a.ExitCode,
		},
	}
	if hasAlertTotal {
		update.AlertTotal = &alertTotal
	}

	m.server.Broadcast(ctx, update)
	m.sendTelegram(ctx, a)
}

func (m *Monitor) sendTelegram(ctx context.Context, a store.Alert) {
	if m.telegram == nil {
		return
	}
	prefix := strings.ToUpper(a.Severity)
	text := fmt.Sprintf("[%s] %s: %s", prefix, a.Container, a.Message)
	if err := m.telegram.Send(ctx, text); err != nil {
		log.Printf("telegram send failed: %v", err)
	}
}

func (m *Monitor) inspectToContainer(inspect container.InspectResponse) store.Container {
	created := parseDockerTime(inspect.Created)
	status := "unknown"
	if inspect.State != nil {
		status = string(inspect.State.Status)
	}

	image := inspect.Config.Image
	imageName, imageTag := parseImage(image)
	caps := resolveCaps(m.capDefault, inspect.HostConfig.CapAdd, inspect.HostConfig.CapDrop)
	user := inspect.Config.User
	if user == "" {
		user = "0:0"
	}
	role := resolveRole(inspect.Config.Labels)
	healthStatus := ""
	healthFailingStreak := 0
	if inspect.State != nil && inspect.State.Health != nil {
		healthStatus = string(inspect.State.Health.Status)
		healthFailingStreak = inspect.State.Health.FailingStreak
	}
	var startedAt time.Time
	if inspect.State != nil {
		startedAt = parseDockerTime(inspect.State.StartedAt)
	}
	var healthcheck *store.Healthcheck
	if inspect.Config != nil && inspect.Config.Healthcheck != nil {
		health := inspect.Config.Healthcheck
		healthcheck = &store.Healthcheck{
			Test:          health.Test,
			Interval:      durationString(health.Interval),
			Timeout:       durationString(health.Timeout),
			StartPeriod:   durationString(health.StartPeriod),
			StartInterval: durationString(health.StartInterval),
			Retries:       health.Retries,
		}
	}

	return store.Container{
		ContainerID:         inspect.ID,
		Image:               imageName,
		ImageTag:            imageTag,
		ImageID:             inspect.Image,
		CreatedAt:           created,
		StartedAt:           startedAt,
		Status:              status,
		Role:                role,
		Caps:                caps,
		ReadOnly:            inspect.HostConfig.ReadonlyRootfs,
		User:                user,
		HealthStatus:        healthStatus,
		HealthFailingStreak: healthFailingStreak,
		Healthcheck:         healthcheck,
		UpdatedAt:           time.Now().UTC(),
		Present:             true,
	}
}

func parseDockerTime(val string) time.Time {
	if val == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, val)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func parseImage(image string) (string, string) {
	if image == "" {
		return "", ""
	}
	ref, err := reference.ParseNormalizedNamed(image)
	if err != nil {
		return image, ""
	}
	ref = reference.TagNameOnly(ref)
	name := ref.Name()
	tag := ""
	if tagged, ok := ref.(reference.NamedTagged); ok {
		tag = tagged.Tag()
	}
	return name, tag
}

func durationString(val time.Duration) string {
	if val <= 0 {
		return ""
	}
	return val.String()
}

func minTime(a, b time.Time) time.Time {
	if a.IsZero() {
		return b
	}
	if b.IsZero() {
		return a
	}
	if a.Before(b) {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func parseRestartCount(details string) int {
	if strings.TrimSpace(details) == "" {
		return 0
	}
	var payload struct {
		RestartCount int `json:"restart_count"`
	}
	if err := json.Unmarshal([]byte(details), &payload); err != nil {
		return 0
	}
	return payload.RestartCount
}

func parseExitCode(val string) *int {
	trimmed := strings.TrimSpace(val)
	if trimmed == "" {
		return nil
	}
	parsed, err := strconv.Atoi(trimmed)
	if err != nil {
		return nil
	}
	return &parsed
}

func isHealthcheckExecEvent(msg events.Message) bool {
	action := string(msg.Action)
	if !strings.HasPrefix(action, "exec_") {
		return false
	}
	cmd := msg.Actor.Attributes["execCommand"]
	if cmd == "" {
		return false
	}
	cmd = strings.ToLower(cmd)
	return strings.Contains(cmd, "healthcheck")
}

func isHealthcheckStatusEvent(msg events.Message) bool {
	return strings.HasPrefix(string(msg.Action), "health_status:")
}

func shouldAlertNoRestartPolicyFailure(reason string, exitCode *int, inspect container.InspectResponse) bool {
	if inspect.HostConfig == nil {
		return false
	}
	if hasAutoRestartPolicy(inspect) {
		return false
	}
	if reason == "oom" {
		return false
	}
	if exitCode == nil {
		return false
	}
	return *exitCode != 0
}

func hasAutoRestartPolicy(inspect container.InspectResponse) bool {
	if inspect.HostConfig == nil {
		return false
	}
	policy := strings.ToLower(strings.TrimSpace(string(inspect.HostConfig.RestartPolicy.Name)))
	return policy != "" && policy != "no"
}

func resolveCaps(defaults, add, drop []string) []string {
	caps := make(map[string]struct{}, len(defaults))
	for _, cap := range defaults {
		caps[cap] = struct{}{}
	}

	dropAll := false
	for _, cap := range drop {
		if strings.ToUpper(cap) == "ALL" {
			dropAll = true
		}
	}
	if dropAll {
		caps = map[string]struct{}{}
	}

	for _, cap := range drop {
		cap = strings.ToUpper(cap)
		delete(caps, cap)
	}
	for _, cap := range add {
		cap = strings.ToUpper(cap)
		caps[cap] = struct{}{}
	}

	out := make([]string, 0, len(caps))
	for cap := range caps {
		out = append(out, cap)
	}
	return out
}

func defaultCaps() []string {
	return []string{
		"CAP_AUDIT_WRITE",
		"CAP_CHOWN",
		"CAP_DAC_OVERRIDE",
		"CAP_FOWNER",
		"CAP_FSETID",
		"CAP_KILL",
		"CAP_MKNOD",
		"CAP_NET_BIND_SERVICE",
		"CAP_NET_RAW",
		"CAP_SETFCAP",
		"CAP_SETGID",
		"CAP_SETPCAP",
		"CAP_SETUID",
		"CAP_SYS_CHROOT",
	}
}

func resolveRole(labels map[string]string) string {
	if labels == nil {
		return "service"
	}
	role := strings.TrimSpace(strings.ToLower(labels["healthmon.role"]))
	if role == "" {
		return "service"
	}
	switch role {
	case "service", "task":
		return role
	default:
		return "service"
	}
}

type restartTracker struct {
	window    time.Duration
	threshold int
	mu        sync.Mutex
	data      map[string][]time.Time
	loop      map[string]bool
}

func newRestartTracker(windowSeconds, threshold int) *restartTracker {
	return &restartTracker{
		window:    time.Duration(windowSeconds) * time.Second,
		threshold: threshold,
		data:      make(map[string][]time.Time),
		loop:      make(map[string]bool),
	}
}

func (r *restartTracker) record(name string, ts time.Time) (int, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	list := r.data[name]
	list = append(list, ts)
	list = r.prune(list, ts)
	r.data[name] = list
	enteredLoop := false
	if len(list) >= r.threshold {
		if !r.loop[name] {
			enteredLoop = true
		}
		r.loop[name] = true
	}
	return len(list), enteredLoop
}

func (r *restartTracker) inLoop(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.loop[name]
}

func (r *restartTracker) canHeal(name string, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	list := r.data[name]
	list = r.prune(list, now)
	r.data[name] = list
	if len(list) == 0 {
		return true
	}
	return now.Sub(list[len(list)-1]) > r.window
}

func (r *restartTracker) markHealed(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.loop, name)
	delete(r.data, name)
}

func (r *restartTracker) markHealthy(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	list := r.data[name]
	if len(list) == 0 {
		return
	}
	last := list[len(list)-1]
	if time.Since(last) > r.window {
		delete(r.loop, name)
	}
}

func (r *restartTracker) reset(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.data, name)
	delete(r.loop, name)
}

func (r *restartTracker) prune(list []time.Time, now time.Time) []time.Time {
	cut := now.Add(-r.window)
	idx := 0
	for idx < len(list) && list[idx].Before(cut) {
		idx++
	}
	return append([]time.Time{}, list[idx:]...)
}
