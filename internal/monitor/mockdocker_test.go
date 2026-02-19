package monitor

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"healthmon/internal/api"
	"healthmon/internal/config"
	"healthmon/internal/db"
	"healthmon/internal/store"

	"github.com/moby/moby/api/types/events"
	"nhooyr.io/websocket"
)

type inspectRecord struct {
	EventIndex int             `json:"event_index"`
	TimeNano   int64           `json:"timeNano"`
	ID         string          `json:"id"`
	Action     string          `json:"action"`
	Inspect    json.RawMessage `json:"inspect"`
}

type inspectQueue struct {
	mu   sync.Mutex
	byID map[string][]inspectRecord
	next map[string]int
}

func newInspectQueue(records []inspectRecord) *inspectQueue {
	byID := make(map[string][]inspectRecord)
	for _, record := range records {
		if record.ID == "" || len(record.Inspect) == 0 {
			continue
		}
		byID[record.ID] = append(byID[record.ID], record)
	}
	return &inspectQueue{byID: byID, next: make(map[string]int)}
}

func (q *inspectQueue) Next(id string) (json.RawMessage, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	records := q.byID[id]
	if len(records) == 0 {
		return nil, false
	}
	idx := q.next[id]
	if idx >= len(records) {
		return records[len(records)-1].Inspect, true
	}
	q.next[id] = idx + 1
	return records[idx].Inspect, true
}

type mockDockerServer struct {
	t          *testing.T
	events     []events.Message
	inspects   *inspectQueue
	httpServer *http.Server
	listener   net.Listener
	doneOnce   sync.Once
	doneCh     chan struct{}
	allowCh    chan struct{}
}

func newMockDockerServer(t *testing.T, events []events.Message, inspects []inspectRecord) *mockDockerServer {
	t.Helper()
	return &mockDockerServer{
		t:        t,
		events:   events,
		inspects: newInspectQueue(inspects),
		doneCh:   make(chan struct{}),
		allowCh:  make(chan struct{}, 1),
	}
}

func (m *mockDockerServer) Start() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	m.listener = listener
	m.httpServer = &http.Server{Handler: http.HandlerFunc(m.handle)}
	go func() {
		_ = m.httpServer.Serve(listener)
	}()
	return "tcp://" + listener.Addr().String(), nil
}

func (m *mockDockerServer) Close() {
	if m.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = m.httpServer.Shutdown(ctx)
		cancel()
	}
	if m.listener != nil {
		_ = m.listener.Close()
	}
}

func (m *mockDockerServer) WaitEventsDone(t *testing.T, timeout time.Duration) {
	t.Helper()
	select {
	case <-m.doneCh:
		return
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for events to stream")
	}
}

func (m *mockDockerServer) AllowNext() {
	m.allowCh <- struct{}{}
}

func (m *mockDockerServer) handle(w http.ResponseWriter, r *http.Request) {
	path := stripDockerVersionPrefix(r.URL.Path)
	switch {
	case path == "/_ping":
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
		return
	case path == "/version":
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ApiVersion":"1.44","MinAPIVersion":"1.12","Version":"29.2.1"}`))
		return
	case path == "/containers/json":
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]"))
		return
	case path == "/events":
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		enc := json.NewEncoder(w)
		for _, msg := range m.events {
			select {
			case <-m.allowCh:
			case <-r.Context().Done():
				break
			}
			if r.Context().Err() != nil {
				break
			}
			if err := enc.Encode(msg); err != nil {
				break
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		m.doneOnce.Do(func() { close(m.doneCh) })
		return
	case strings.HasPrefix(path, "/containers/") && strings.HasSuffix(path, "/json"):
		id := strings.TrimSuffix(strings.TrimPrefix(path, "/containers/"), "/json")
		raw, ok := m.inspects.Next(id)
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
		return
	default:
		http.NotFound(w, r)
	}
}

var dockerVersionPrefix = regexp.MustCompile(`^/v[0-9]+\.[0-9]+`)

func stripDockerVersionPrefix(path string) string {
	loc := dockerVersionPrefix.FindStringIndex(path)
	if loc == nil || loc[0] != 0 {
		return path
	}
	stripped := path[loc[1]:]
	if stripped == "" {
		return "/"
	}
	return stripped
}

func loadEventsJSONL(path string) ([]events.Message, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	out := []events.Message{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var msg events.Message
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			return nil, fmt.Errorf("parse event: %w", err)
		}
		out = append(out, msg)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func loadInspectJSONL(path string) ([]inspectRecord, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 20*1024*1024)
	out := []inspectRecord{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var record inspectRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return nil, fmt.Errorf("parse inspect: %w", err)
		}
		out = append(out, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func buildNameIDIndex(messages []events.Message) map[string]map[string]struct{} {
	index := make(map[string]map[string]struct{})
	add := func(name, id string) {
		if name == "" || id == "" {
			return
		}
		name = strings.TrimPrefix(name, "/")
		ids := index[name]
		if ids == nil {
			ids = make(map[string]struct{})
			index[name] = ids
		}
		ids[id] = struct{}{}
	}

	for _, msg := range messages {
		add(msg.Actor.Attributes["name"], msg.Actor.ID)
		add(msg.Actor.Attributes["oldName"], msg.Actor.ID)
		add(msg.Actor.Attributes["old_name"], msg.Actor.ID)
	}
	return index
}

func resolveReplayPaths() (string, string, error) {
	eventsPath := os.Getenv("TEST_DOCKER_EVENTS")
	if eventsPath == "" {
		scenario := os.Getenv("TEST_DOCKER_SCENARIO")
		if scenario == "" {
			scenario = "basic"
		}
		eventsPath = filepath.Join("testdata", "dumps", fmt.Sprintf("%s.events.jsonl", scenario))
	}
	inspectsPath := os.Getenv("TEST_DOCKER_INSPECTS")
	if inspectsPath == "" {
		scenario := os.Getenv("TEST_DOCKER_SCENARIO")
		if scenario == "" {
			scenario = "basic"
		}
		inspectsPath = filepath.Join("testdata", "dumps", fmt.Sprintf("%s.inspects.jsonl", scenario))
	}
	if _, err := os.Stat(eventsPath); err != nil {
		return "", "", err
	}
	if _, err := os.Stat(inspectsPath); err != nil {
		return "", "", err
	}
	return eventsPath, inspectsPath, nil
}

func startMonitorWithReplay(t *testing.T, events []events.Message, inspects []inspectRecord) (*store.Store, *mockDockerServer, *httptest.Server, func()) {
	t.Helper()
	mock := newMockDockerServer(t, events, inspects)
	host, err := mock.Start()
	if err != nil {
		t.Fatalf("start mock docker: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "healthmon.db")
	dbConn, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	ctx := context.Background()
	if err := dbConn.Migrate(ctx); err != nil {
		_ = dbConn.Close()
		t.Fatalf("migrate db: %v", err)
	}

	st := store.New(dbConn.SQL)
	if err := st.Load(ctx); err != nil {
		_ = dbConn.Close()
		t.Fatalf("load store: %v", err)
	}

	srv := api.NewServer(st, api.NewBroadcaster(), api.WSOptions{})
	httpServer := httptest.NewServer(srv.Routes())
	mon := New(config.Config{
		DockerHost:           host,
		RestartWindowSeconds: 30,
		RestartThreshold:     3,
	}, st, srv)

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		done <- mon.Start(runCtx)
	}()

	cleanup := func() {
		httpServer.Close()
		mock.Close()
		cancel()
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, io.EOF) {
				t.Fatalf("monitor error: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("monitor did not exit")
		}
		_ = dbConn.Close()
	}
	return st, mock, httpServer, cleanup
}

func TestMonitorReplayLinksEvents(t *testing.T) {
	eventsPath, inspectsPath, err := resolveReplayPaths()
	if err != nil {
		t.Skipf("replay data missing: %v", err)
	}

	messages, err := loadEventsJSONL(eventsPath)
	if err != nil {
		t.Fatalf("load events: %v", err)
	}
	records, err := loadInspectJSONL(inspectsPath)
	if err != nil {
		t.Fatalf("load inspects: %v", err)
	}

	st, mock, httpServer, cleanup := startMonitorWithReplay(t, messages, records)
	defer cleanup()

	ctx := context.Background()
	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/api/events/stream"
	wsConn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	defer wsConn.Close(websocket.StatusNormalClosure, "closing")

	nameIDs := buildNameIDIndex(messages)
	expectEmit := map[string]struct{}{
		"create":  {},
		"start":   {},
		"die":     {},
		"restart": {},
		"oom":     {},
	}

	for _, msg := range messages {
		mock.AllowNext()

		action := strings.ToLower(string(msg.Action))
		if _, ok := expectEmit[action]; !ok {
			continue
		}

		update, err := readWSUpdate(ctx, wsConn)
		if err != nil {
			t.Fatalf("ws read: %v", err)
		}

		expectedName := strings.TrimPrefix(msg.Actor.Attributes["name"], "/")
		expectedID := msg.Actor.ID
		if update.Event.Container != expectedName {
			t.Fatalf("expected event container %q got %q", expectedName, update.Event.Container)
		}
		if update.Event.ContainerID != expectedID {
			t.Fatalf("expected event id %q got %q", expectedID, update.Event.ContainerID)
		}

		ids := nameIDs[expectedName]
		if ids == nil {
			t.Fatalf("missing name mapping for %q", expectedName)
		}
		if _, ok := ids[expectedID]; !ok {
			t.Fatalf("event mapped to %q id %q not seen in replay", expectedName, expectedID)
		}

		resp, err := http.Get(httpServer.URL + "/api/containers")
		if err != nil {
			t.Fatalf("get containers: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("containers status %d", resp.StatusCode)
		}
		var containers []api.ContainerResponse
		if err := json.NewDecoder(resp.Body).Decode(&containers); err != nil {
			resp.Body.Close()
			t.Fatalf("decode containers: %v", err)
		}
		resp.Body.Close()

		found := false
		for _, c := range containers {
			if c.Name == expectedName {
				found = true
				if c.ContainerID != expectedID {
					t.Fatalf("container %q id mismatch: %q != %q", expectedName, c.ContainerID, expectedID)
				}
			}
		}
		if !found {
			t.Fatalf("expected container %q not found in api response", expectedName)
		}
	}

	mock.WaitEventsDone(t, 5*time.Second)

	eventsList, err := st.ListAllEvents(ctx, 0, 5000)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(eventsList) == 0 {
		t.Fatalf("expected events from replay, got none")
	}
}

func readWSUpdate(ctx context.Context, conn *websocket.Conn) (api.EventUpdate, error) {
	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, data, err := conn.Read(readCtx)
	if err != nil {
		return api.EventUpdate{}, err
	}
	var update api.EventUpdate
	if err := json.Unmarshal(data, &update); err != nil {
		return api.EventUpdate{}, err
	}
	return update, nil
}
