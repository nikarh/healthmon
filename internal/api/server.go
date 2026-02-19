package api

import (
	"context"
	"encoding/json"
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
}

func NewServer(store *store.Store, broadcaster *Broadcaster) *Server {
	return &Server{store: store, broadcaster: broadcaster}
}

func (s *Server) WithStatic(fs http.FileSystem) {
	s.staticFS = fs
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/containers", s.handleContainers)
	mux.HandleFunc("/api/containers/", s.handleContainerEvents)
	mux.HandleFunc("/api/events", s.handleEvents)
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
	ctx := r.Context()
	for _, c := range items {
		var lastEvent *EventResponse
		if c.LastEventID > 0 {
			event, ok, err := s.store.GetEvent(ctx, c.LastEventID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if ok {
				lastEvent = toEventResponse(event)
			}
		} else if c.ID > 0 {
			event, ok, err := s.store.GetLatestEventByContainerPK(ctx, c.ID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if ok {
				lastEvent = toEventResponse(event)
			}
		}
		resp = append(resp, toContainerResponse(c, lastEvent))
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

	resp := make([]EventResponse, 0, len(items))
	for _, e := range items {
		resp = append(resp, *toEventResponse(e))
	}

	writeJSON(w, http.StatusOK, resp)
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

	resp := make([]EventResponse, 0, len(items))
	for _, e := range items {
		resp = append(resp, *toEventResponse(e))
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
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
	ID          int64          `json:"id"`
	Name        string         `json:"name"`
	ContainerID string         `json:"container_id"`
	Image       string         `json:"image"`
	ImageTag    string         `json:"image_tag"`
	ImageID     string         `json:"image_id"`
	CreatedAt   string         `json:"created_at"`
	FirstSeenAt string         `json:"first_seen_at"`
	Status      string         `json:"status"`
	Role        string         `json:"role"`
	Caps        []string       `json:"caps"`
	ReadOnly    bool           `json:"read_only"`
	User        string         `json:"user"`
	LastEvent   *EventResponse `json:"last_event"`
}

type EventResponse struct {
	ID          int64  `json:"id"`
	ContainerPK int64  `json:"container_pk"`
	Container   string `json:"container"`
	ContainerID string `json:"container_id"`
	Type        string `json:"type"`
	Severity    string `json:"severity"`
	Message     string `json:"message"`
	Timestamp   string `json:"timestamp"`
	OldImage    string `json:"old_image"`
	NewImage    string `json:"new_image"`
	OldImageID  string `json:"old_image_id"`
	NewImageID  string `json:"new_image_id"`
	Reason      string `json:"reason"`
	DetailsJSON string `json:"details"`
}

type EventUpdate struct {
	Container ContainerResponse `json:"container"`
	Event     EventResponse     `json:"event"`
}

func toContainerResponse(c store.Container, lastEvent *EventResponse) ContainerResponse {
	return ContainerResponse{
		ID:          c.ID,
		Name:        c.Name,
		ContainerID: c.ContainerID,
		Image:       c.Image,
		ImageTag:    c.ImageTag,
		ImageID:     c.ImageID,
		CreatedAt:   c.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		FirstSeenAt: c.FirstSeenAt.UTC().Format("2006-01-02T15:04:05Z"),
		Status:      c.Status,
		Role:        c.Role,
		Caps:        c.Caps,
		ReadOnly:    c.ReadOnly,
		User:        c.User,
		LastEvent:   lastEvent,
	}
}

func toEventResponse(e store.Event) *EventResponse {
	return &EventResponse{
		ID:          e.ID,
		ContainerPK: e.ContainerPK,
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
