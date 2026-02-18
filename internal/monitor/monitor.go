package monitor

import (
	"context"
	"fmt"
	"log"
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

	for _, c := range result.Items {
		if len(c.Names) == 0 {
			continue
		}
		name := strings.TrimPrefix(c.Names[0], "/")
		inspect, err := m.docker.ContainerInspect(ctx, c.ID, client.ContainerInspectOptions{})
		if err != nil {
			continue
		}
		info := m.inspectToContainer(inspect.Container)
		info.Name = name
		if existing, ok := m.store.GetContainer(name); ok {
			info.FirstSeenAt = existing.FirstSeenAt
		}
		if info.FirstSeenAt.IsZero() {
			info.FirstSeenAt = time.Now().UTC()
		}
		if err := m.store.UpsertContainer(ctx, info); err != nil {
			return err
		}
	}
	return nil
}

func (m *Monitor) handleEvent(ctx context.Context, msg events.Message) {
	name := msg.Actor.Attributes["name"]
	if name == "" {
		return
	}
	name = strings.TrimPrefix(name, "/")
	log.Printf("event: container=%s action=%s id=%s", name, msg.Action, msg.Actor.ID)

	switch {
	case msg.Action == "create":
		m.handleCreate(ctx, name, msg.Actor.ID)
	case msg.Action == "start":
		m.handleStart(ctx, name, msg.Actor.ID)
	case msg.Action == "die":
		m.handleRestartLike(ctx, name, msg.Actor.ID, "die")
	case msg.Action == "restart":
		m.handleRestartLike(ctx, name, msg.Actor.ID, "restart")
	case msg.Action == "kill":
		m.handleRestartLike(ctx, name, msg.Actor.ID, "kill")
	case msg.Action == "oom":
		m.handleRestartLike(ctx, name, msg.Actor.ID, "oom")
	case strings.HasPrefix(string(msg.Action), "health_status:"):
		m.handleHealth(ctx, name, msg.Actor.ID, strings.TrimSpace(strings.TrimPrefix(string(msg.Action), "health_status:")))
	case msg.Action == "destroy":
		_ = m.store.DeleteContainer(ctx, name)
	}
}

func (m *Monitor) handleCreate(ctx context.Context, name, id string) {
	inspect, err := m.docker.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if err != nil {
		return
	}

	newInfo := m.inspectToContainer(inspect.Container)
	newInfo.Name = name

	existing, has := m.store.GetContainer(name)
	if has {
		newInfo.FirstSeenAt = existing.FirstSeenAt
	} else {
		newInfo.FirstSeenAt = time.Now().UTC()
	}

	if has && existing.ContainerID != id {
		imageChanged := existing.ImageID != newInfo.ImageID || existing.ImageTag != newInfo.ImageTag
		if imageChanged {
			m.emitInfo(ctx, name, id, "image_changed", fmt.Sprintf("Image changed %s -> %s", existing.Image, newInfo.Image), existing.Image, newInfo.Image, existing.ImageID, newInfo.ImageID, "recreate")
		} else {
			m.emitInfo(ctx, name, id, "recreated", "Container recreated", existing.Image, newInfo.Image, existing.ImageID, newInfo.ImageID, "recreate")
		}
	}

	_ = m.store.UpsertContainer(ctx, newInfo)
}

func (m *Monitor) handleStart(ctx context.Context, name, id string) {
	inspect, err := m.docker.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if err != nil {
		return
	}
	info := m.inspectToContainer(inspect.Container)
	info.Name = name
	if existing, ok := m.store.GetContainer(name); ok {
		info.FirstSeenAt = existing.FirstSeenAt
	}
	if info.FirstSeenAt.IsZero() {
		info.FirstSeenAt = time.Now().UTC()
	}
	_ = m.store.UpsertContainer(ctx, info)
}

func (m *Monitor) handleHealth(ctx context.Context, name, id, status string) {
	if status == "unhealthy" {
		m.handleRestartLike(ctx, name, id, "health_unhealthy")
		return
	}
	if status == "healthy" {
		m.restarts.markHealthy(name)
	}
}

func (m *Monitor) handleRestartLike(ctx context.Context, name, id, reason string) {
	now := time.Now().UTC()

	m.restarts.record(name, now)
	m.emitInfo(ctx, name, id, "restart", fmt.Sprintf("Restart event: %s", reason), "", "", "", "", reason)

	if m.restarts.justEnteredLoop(name) {
		m.emitAlert(ctx, name, id, "restart_loop", "Restart loop detected", "red")
	}

	inspect, err := m.docker.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if err == nil {
		info := m.inspectToContainer(inspect.Container)
		info.Name = name
		if existing, ok := m.store.GetContainer(name); ok {
			info.FirstSeenAt = existing.FirstSeenAt
		}
		if info.FirstSeenAt.IsZero() {
			info.FirstSeenAt = now
		}
		_ = m.store.UpsertContainer(ctx, info)
	}
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
	for _, name := range m.store.ContainerNames() {
		if !m.restarts.inLoop(name) {
			continue
		}
		if !m.restarts.canHeal(name, now) {
			continue
		}
		if c, ok := m.store.GetContainer(name); ok {
			if strings.ToLower(c.Status) == "running" {
				m.restarts.markHealed(name)
				m.emitAlert(ctx, name, c.ContainerID, "restart_healed", "Restart loop healed", "green")
			}
		}
	}
}

func (m *Monitor) emitInfo(ctx context.Context, name, id, eventType, message, oldImage, newImage, oldImageID, newImageID, reason string) {
	m.emit(ctx, store.Event{
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
	})
}

func (m *Monitor) emitAlert(ctx context.Context, name, id, eventType, message, severity string) {
	m.emit(ctx, store.Event{
		Container:   name,
		ContainerID: id,
		Type:        eventType,
		Severity:    severity,
		Message:     message,
		Timestamp:   time.Now().UTC(),
	})
}

func (m *Monitor) emit(ctx context.Context, e store.Event) {
	container, ok := m.store.GetContainer(e.Container)
	if !ok {
		return
	}
	e.ContainerPK = container.ID
	log.Printf("event: type=%s severity=%s container=%s", e.Type, e.Severity, e.Container)
	id, err := m.store.AddEvent(ctx, e)
	if err != nil {
		log.Printf("event persist failed: %v", err)
		return
	}
	e.ID = id

	update := api.EventUpdate{
		Container: api.ContainerResponse{
			ID:          container.ID,
			Name:        container.Name,
			ContainerID: container.ContainerID,
			Image:       container.Image,
			ImageTag:    container.ImageTag,
			ImageID:     container.ImageID,
			CreatedAt:   container.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
			FirstSeenAt: container.FirstSeenAt.UTC().Format("2006-01-02T15:04:05Z"),
			Status:      container.Status,
			Caps:        container.Caps,
			ReadOnly:    container.ReadOnly,
			User:        container.User,
			LastEvent:   &api.EventResponse{ID: id, Type: e.Type, Severity: e.Severity, Message: e.Message, Timestamp: e.Timestamp.UTC().Format("2006-01-02T15:04:05Z")},
		},
		Event: api.EventResponse{
			ID:          e.ID,
			ContainerPK: container.ID,
			Container:   e.Container,
			ContainerID: e.ContainerID,
			Type:        e.Type,
			Severity:    e.Severity,
			Message:     e.Message,
			Timestamp:   e.Timestamp.UTC().Format("2006-01-02T15:04:05Z"),
			OldImage:    e.OldImage,
			NewImage:    e.NewImage,
			OldImageID:  e.OldImageID,
			NewImageID:  e.NewImageID,
			Reason:      e.Reason,
			DetailsJSON: e.DetailsJSON,
		},
	}

	m.server.Broadcast(ctx, update)
	if shouldAlert(e.Type) {
		m.sendTelegram(ctx, e)
	}
}

func (m *Monitor) sendTelegram(ctx context.Context, e store.Event) {
	if m.telegram == nil {
		return
	}
	prefix := strings.ToUpper(e.Severity)
	text := fmt.Sprintf("[%s] %s: %s", prefix, e.Container, e.Message)
	if err := m.telegram.Send(ctx, text); err != nil {
		log.Printf("telegram send failed: %v", err)
	}
}

func shouldAlert(eventType string) bool {
	switch eventType {
	case "restart_loop", "restart_healed", "image_changed", "recreated":
		return true
	default:
		return false
	}
}

func (m *Monitor) inspectToContainer(inspect container.InspectResponse) store.Container {
	created := parseDockerTime(inspect.Created)
	status := "unknown"
	if inspect.State != nil {
		status = string(inspect.State.Status)
		if inspect.State.OOMKilled {
			status = "oom"
		} else if inspect.State.Status == "exited" && inspect.State.ExitCode != 0 {
			status = "crashed"
		}
	}

	image := inspect.Config.Image
	imageName, imageTag := parseImage(image)
	caps := resolveCaps(m.capDefault, inspect.HostConfig.CapAdd, inspect.HostConfig.CapDrop)
	user := inspect.Config.User
	if user == "" {
		user = "0:0"
	}

	return store.Container{
		ContainerID: inspect.ID,
		Image:       imageName,
		ImageTag:    imageTag,
		ImageID:     inspect.Image,
		CreatedAt:   created,
		Status:      status,
		Caps:        caps,
		ReadOnly:    inspect.HostConfig.ReadonlyRootfs,
		User:        user,
		UpdatedAt:   time.Now().UTC(),
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
	named := ref.(reference.Named)
	name := named.Name()
	tag := ""
	if tagged, ok := named.(reference.NamedTagged); ok {
		tag = tagged.Tag()
	}
	return name, tag
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

func (r *restartTracker) record(name string, ts time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	list := r.data[name]
	list = append(list, ts)
	list = r.prune(list, ts)
	r.data[name] = list
	if len(list) >= r.threshold {
		r.loop[name] = true
	}
}

func (r *restartTracker) justEnteredLoop(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.loop[name] && len(r.data[name]) == r.threshold
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

func (r *restartTracker) prune(list []time.Time, now time.Time) []time.Time {
	cut := now.Add(-r.window)
	idx := 0
	for idx < len(list) && list[idx].Before(cut) {
		idx++
	}
	return append([]time.Time{}, list[idx:]...)
}
