package audiotap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const acceptTimeout = 45 * time.Second

type Options struct {
	AppPath  string
	BundleID string
	BitRate  int
}

type Tap struct {
	conn     net.Conn
	listener net.Listener
	dir      string
	logPath  string
	once     sync.Once
	closeErr error
}

func Start(ctx context.Context, opts Options, logger *slog.Logger) (*Tap, error) {
	dir, err := os.MkdirTemp("", "cloudphone-audio-")
	if err != nil {
		return nil, err
	}
	t := &Tap{dir: dir, logPath: filepath.Join(dir, "audiotap.log")}
	socketPath := filepath.Join(dir, "audio.sock")
	if t.listener, err = net.Listen("unix", socketPath); err != nil {
		t.Close()
		return nil, fmt.Errorf("audiotap: listen: %w", err)
	}

	launch := exec.CommandContext(ctx, "open", "-n", "-g", "--stderr", t.logPath, opts.AppPath, "--args",
		"--socket", socketPath, "--bundle", opts.BundleID, "--bitrate", strconv.Itoa(opts.BitRate))
	if out, err := launch.CombinedOutput(); err != nil {
		t.Close()
		return nil, fmt.Errorf("audiotap: launch %s: %w: %s", opts.AppPath, err, out)
	}
	logger.Info("audio helper launched", "app", opts.AppPath, "bundle", opts.BundleID, "log", t.logPath)

	accepted := make(chan error, 1)
	go func() {
		conn, err := t.listener.Accept()
		t.conn = conn
		accepted <- err
	}()
	select {
	case err := <-accepted:
		if err != nil {
			t.Close()
			return nil, fmt.Errorf("audiotap: accept: %w", err)
		}
	case <-time.After(acceptTimeout):
		helperLog, _ := os.ReadFile(t.logPath)
		t.Close()
		return nil, fmt.Errorf("audiotap: helper did not connect within %s: %s", acceptTimeout, helperLog)
	case <-ctx.Done():
		t.Close()
		return nil, ctx.Err()
	}
	logger.Info("audio helper connected")
	return t, nil
}

func (t *Tap) ReadPacket() (Packet, error) {
	return ReadPacket(t.conn)
}

func (t *Tap) Close() error {
	t.once.Do(func() { t.closeErr = t.close() })
	return t.closeErr
}

func (t *Tap) close() error {
	var errs []error
	if t.conn != nil {
		errs = append(errs, t.conn.Close())
	}
	if t.listener != nil {
		errs = append(errs, t.listener.Close())
	}
	errs = append(errs, os.RemoveAll(t.dir))
	return errors.Join(errs...)
}
