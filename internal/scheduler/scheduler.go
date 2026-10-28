// Package scheduler runs the built-in periodic jobs.
package scheduler

import (
	"context"
	"time"
)

// Every runs job now and then on each tick until ctx ends.
func Every(ctx context.Context, d time.Duration, job func(context.Context)) {
	t := time.NewTicker(d)
	defer t.Stop()
	for {
		job(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
