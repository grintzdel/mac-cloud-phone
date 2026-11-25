package supervise

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestRunRestartsAfterFailureUntilCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	Run(ctx, "test", Backoff{Delays: []time.Duration{time.Millisecond}}, quiet, func(context.Context) error {
		calls++
		if calls == 3 {
			cancel()
		}
		return errors.New("boom")
	})
	if calls != 3 {
		t.Fatalf("got %d calls", calls)
	}
}

func TestRunStopsWaitingWhenCancelledDuringBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	start := time.Now()
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	Run(ctx, "test", Backoff{Delays: []time.Duration{time.Hour}}, quiet, func(context.Context) error {
		return errors.New("boom")
	})
	if time.Since(start) > time.Second {
		t.Fatal("backoff did not observe cancellation")
	}
}

func TestBackoffGrowsThenCaps(t *testing.T) {
	b := Backoff{Delays: []time.Duration{1, 2, 5}}
	var got []time.Duration
	for i := 0; i < 5; i++ {
		got = append(got, b.next())
	}
	want := []time.Duration{1, 2, 5, 5, 5}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v", got)
		}
	}
}

func TestBackoffResetsAfterAHealthyRun(t *testing.T) {
	b := Backoff{Delays: []time.Duration{1, 2, 5}, HealthyAfter: time.Minute}
	b.next()
	b.next()
	b.observe(2 * time.Minute)
	if d := b.next(); d != 1 {
		t.Fatalf("expected reset to first delay, got %v", d)
	}
}
