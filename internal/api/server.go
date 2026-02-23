package api

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"healthmon/internal/store"

	"nhooyr.io/websocket"
)

type Server struct {
	store       *store.Store
	broadcaster *Broadcaster
	staticFS    http.FileSystem
	wsOptions   WSOptions
}

type WSOptions struct {
	OriginPatterns     []string
	InsecureSkipVerify bool
}

func NewServer(store *store.Store, broadcaster *Broadcaster, wsOptions WSOptions) *Server {
	return &Server{store: store, broadcaster: broadcaster, wsOptions: wsOptions}
}

func (s *Server) WithStatic(fs http.FileSystem) {
	s.staticFS = fs
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/containers", s.handleContainers)
	mux.HandleFunc("/api/containers/", s.handleContainerEvents)
	mux.HandleFunc("/api/events", s.handleEvents)
	mux.HandleFunc("/api/alerts", s.handleAlerts)
	mux.HandleFunc("/api/events/stream", s.handleStream)

	if s.staticFS != nil {
		mux.Handle("/", http.HandlerFunc(s.handleSPA))
	}

	return loggingMiddleware(mux)
}

func (s *Server) handleSPA(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path == "/" {
		r.URL.Path = "/index.html"
	} else {
		if path != "/" && strings.HasSuffix(path, "/") {
			r.URL.Path = strings.TrimSuffix(path, "/")
		}
	}

	file, err := s.staticFS.Open(r.URL.Path)
	if err != nil {
		index, err := s.staticFS.Open("/index.html")
		if err != nil {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		defer index.Close()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.Copy(w, index)
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err == nil && info.IsDir() {
		index, err := s.staticFS.Open("/index.html")
		if err != nil {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		defer index.Close()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.Copy(w, index)
		return
	}

	http.ServeContent(w, r, r.URL.Path, info.ModTime(), file)
}

func (s *Server) handleContainers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	items := s.store.ListContainers()
	resp := make([]ContainerResponse, 0, len(items))
	for _, c := range items {
		resp = append(resp, toContainerResponse(c))
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleContainerEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/containers/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || parts[1] != "events" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	name := parts[0]
	beforeID, _ := strconv.ParseInt(r.URL.Query().Get("before_id"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	items, err := s.store.ListEvents(r.Context(), name, beforeID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	total, err := s.store.CountEventsByContainer(r.Context(), name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	resp := make([]EventResponse, 0, len(items))
	for _, e := range items {
		resp = append(resp, *toEventResponse(e))
	}

	writeJSON(w, http.StatusOK, EventListResponse{Items: resp, Total: total})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	beforeID, _ := strconv.ParseInt(r.URL.Query().Get("before_id"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	items, err := s.store.ListAllEvents(r.Context(), beforeID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	total, err := s.store.CountAllEvents(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	resp := make([]EventResponse, 0, len(items))
	for _, e := range items {
		resp = append(resp, *toEventResponse(e))
	}

	writeJSON(w, http.StatusOK, EventListResponse{Items: resp, Total: total})
}

func (s *Server) handleAlerts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	beforeID, _ := strconv.ParseInt(r.URL.Query().Get("before_id"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	items, err := s.store.ListAllAlerts(r.Context(), beforeID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	total, err := s.store.CountAllAlerts(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	resp := make([]AlertResponse, 0, len(items))
	for _, a := range items {
		resp = append(resp, *toAlertResponse(a))
	}

	writeJSON(w, http.StatusOK, AlertListResponse{Items: resp, Total: total})
}

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns:     s.wsOptions.OriginPatterns,
		InsecureSkipVerify: s.wsOptions.InsecureSkipVerify,
	})
	if err != nil {
		return
	}
	peer := clientIP(r)
	log.Printf("ws connect: %s", peer)
	defer func() {
		log.Printf("ws disconnect: %s", peer)
		conn.Close(websocket.StatusNormalClosure, "closing")
	}()

	s.broadcaster.Add(conn)
	defer s.broadcaster.Remove(conn)

	ctx := r.Context()
	for {
		_, _, err := conn.Read(ctx)
		if err != nil {
			return
		}
	}
}

func (s *Server) Broadcast(ctx context.Context, update EventUpdate) {
	payload, err := json.Marshal(update)
	if err != nil {
		return
	}
	s.broadcaster.Broadcast(ctx, payload)
}

type ContainerResponse struct {
	ID                  int64              `json:"id"`
	Name                string             `json:"name"`
	ContainerID         string             `json:"container_id"`
	Image               string             `json:"image"`
	ImageTag            string             `json:"image_tag"`
	ImageID             string             `json:"image_id"`
	CreatedAt           string             `json:"created_at"`
	RegisteredAt        string             `json:"registered_at"`
	StartedAt           string             `json:"started_at"`
	Status              string             `json:"status"`
	Role                string             `json:"role"`
	Caps                []string           `json:"caps"`
	ReadOnly            bool               `json:"read_only"`
	NoNewPrivileges     bool               `json:"no_new_privileges"`
	User                string             `json:"user"`
	Present             bool               `json:"present"`
	HealthStatus        string             `json:"health_status"`
	HealthFailingStreak int                `json:"health_failing_streak"`
	UnhealthySince      string             `json:"unhealthy_since"`
	RestartLoop         bool               `json:"restart_loop"`
	RestartStreak       int                `json:"restart_streak"`
	RestartLoopSince    string             `json:"restart_loop_since"`
	Healthcheck         *store.Healthcheck `json:"healthcheck"`
}

type EventResponse struct {
	ID          int64  `json:"id"`
	ContainerPK int64  `json:"container_pk"`
	Container   string `json:"container"`
	ContainerID string `json:"container_id"`
	Type        string `json:"type"`
	Message     string `json:"message"`
	Timestamp   string `json:"timestamp"`
	OldImage    string `json:"old_image"`
	NewImage    string `json:"new_image"`
	OldImageID  string `json:"old_image_id"`
	NewImageID  string `json:"new_image_id"`
	Reason      string `json:"reason"`
	DetailsJSON string `json:"details"`
	ExitCode    *int   `json:"exit_code"`
}

type EventListResponse struct {
	Items []EventResponse `json:"items"`
	Total int64           `json:"total"`
}

type AlertResponse struct {
	ID          int64  `json:"id"`
	ContainerPK int64  `json:"container_pk"`
	Container   string `json:"container"`
	ContainerID string `json:"container_id"`
	Type        string `json:"type"`
	Message     string `json:"message"`
	Timestamp   string `json:"timestamp"`
	OldImage    string `json:"old_image"`
	NewImage    string `json:"new_image"`
	OldImageID  string `json:"old_image_id"`
	NewImageID  string `json:"new_image_id"`
	Reason      string `json:"reason"`
	DetailsJSON string `json:"details"`
	ExitCode    *int   `json:"exit_code"`
}

type AlertListResponse struct {
	Items []AlertResponse `json:"items"`
	Total int64           `json:"total"`
}

type EventUpdate struct {
	Container           ContainerResponse `json:"container"`
	Event               *EventResponse    `json:"event,omitempty"`
	Alert               *AlertResponse    `json:"alert,omitempty"`
	ContainerEventTotal *int64            `json:"container_event_total,omitempty"`
	EventTotal          *int64            `json:"event_total,omitempty"`
	AlertTotal          *int64            `json:"alert_total,omitempty"`
}

func toContainerResponse(c store.Container) ContainerResponse {
	return ContainerResponse{
		ID:                  c.ID,
		Name:                c.Name,
		ContainerID:         c.ContainerID,
		Image:               c.Image,
		ImageTag:            c.ImageTag,
		ImageID:             c.ImageID,
		CreatedAt:           c.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		RegisteredAt:        c.RegisteredAt.UTC().Format("2006-01-02T15:04:05Z"),
		StartedAt:           c.StartedAt.UTC().Format("2006-01-02T15:04:05Z"),
		Status:              c.Status,
		Role:                c.Role,
		Caps:                c.Caps,
		ReadOnly:            c.ReadOnly,
		NoNewPrivileges:     c.NoNewPrivileges,
		User:                c.User,
		Present:             c.Present,
		HealthStatus:        c.HealthStatus,
		HealthFailingStreak: c.HealthFailingStreak,
		UnhealthySince:      c.UnhealthySince.UTC().Format("2006-01-02T15:04:05Z"),
		RestartLoop:         c.RestartLoop,
		RestartStreak:       c.RestartStreak,
		RestartLoopSince:    c.RestartLoopSince.UTC().Format("2006-01-02T15:04:05Z"),
		Healthcheck:         c.Healthcheck,
	}
}

func toEventResponse(e store.Event) *EventResponse {
	return &EventResponse{
		ID:          e.ID,
		ContainerPK: e.ContainerPK,
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
	}
}

func toAlertResponse(a store.Alert) *AlertResponse {
	return &AlertResponse{
		ID:          a.ID,
		ContainerPK: a.ContainerPK,
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
	}
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := s.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("response writer does not support hijacking")
	}
	return hijacker.Hijack()
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Printf("http %s %s %d %s", r.Method, r.URL.Path, rec.status, time.Since(start))
	})
}

func clientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		return strings.TrimSpace(parts[0])
	}
	if real := r.Header.Get("X-Real-Ip"); real != "" {
		return real
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return ip
	}
	return r.RemoteAddr
}
