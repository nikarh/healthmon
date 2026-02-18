package api

import (
	"context"
	"sync"
	"time"

	"nhooyr.io/websocket"
)

type Broadcaster struct {
	mu    sync.Mutex
	conns map[*websocket.Conn]struct{}
}

func NewBroadcaster() *Broadcaster {
	return &Broadcaster{conns: make(map[*websocket.Conn]struct{})}
}

func (b *Broadcaster) Add(conn *websocket.Conn) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.conns[conn] = struct{}{}
}

func (b *Broadcaster) Remove(conn *websocket.Conn) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.conns, conn)
}

func (b *Broadcaster) Broadcast(ctx context.Context, payload []byte) {
	b.mu.Lock()
	conns := make([]*websocket.Conn, 0, len(b.conns))
	for conn := range b.conns {
		conns = append(conns, conn)
	}
	b.mu.Unlock()

	for _, conn := range conns {
		writeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		_ = conn.Write(writeCtx, websocket.MessageText, payload)
		cancel()
	}
}
