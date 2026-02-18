package docker

import (
	"context"
	"fmt"

	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/client"
)

type Watcher struct {
	socketPath string
}

func NewWatcher(socketPath string) *Watcher {
	return &Watcher{socketPath: socketPath}
}

func (w *Watcher) Start(ctx context.Context, handler func(events.Message)) error {
	host := fmt.Sprintf("unix://%s", w.socketPath)
	cli, err := client.NewClientWithOpts(client.WithHost(host), client.WithAPIVersionNegotiation())
	if err != nil {
		return err
	}

	msgs, errs := cli.Events(ctx, events.ListOptions{})
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errs:
			return err
		case msg := <-msgs:
			handler(msg)
		}
	}
}
