package supervise

import (
	"context"
	"log/slog"
	"time"
)

type Backoff struct {
	Delays       []time.Duration
	HealthyAfter time.Duration
	attempt      int
}

func (b *Backoff) next() time.Duration {
	d := b.Delays[min(b.attempt, len(b.Delays)-1)]
	b.attempt++
	return d
}

func (b *Backoff) observe(ran time.Duration) {
	if b.HealthyAfter > 0 && ran >= b.HealthyAfter {
		b.attempt = 0
	}
}

func Run(ctx context.Context, name string, backoff Backoff, logger *slog.Logger, run func(context.Context) error) {
	for ctx.Err() == nil {
		started := time.Now()
		err := run(ctx)
		if ctx.Err() != nil {
			return
		}
		backoff.observe(time.Since(started))
		delay := backoff.next()
		logger.Warn(name+" stopped, restarting", "err", err, "in", delay)
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}
