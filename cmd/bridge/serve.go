package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"cloudphone/internal/adb"
	"cloudphone/internal/audiotap"
	"cloudphone/internal/control"
	"cloudphone/internal/scrcpy"
	"cloudphone/internal/signaling"
	"cloudphone/internal/supervise"
	"cloudphone/internal/webrtc"
	"cloudphone/web"
)

var restartBackoff = supervise.Backoff{
	Delays:       []time.Duration{time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second},
	HealthyAfter: 30 * time.Second,
}

type device struct {
	mu     sync.Mutex
	server *scrcpy.Server
	size   scrcpy.VideoSize
}

func (d *device) current() *scrcpy.Server {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.server
}

func (d *device) set(s *scrcpy.Server) {
	d.mu.Lock()
	d.server = s
	d.mu.Unlock()
}

func serve(ctx context.Context, cfg config, logger *slog.Logger) error {
	token, err := signaling.LoadOrCreateToken(cfg.tokenFile)
	if err != nil {
		return fmt.Errorf("token: %w", err)
	}

	var dev device
	streamer, err := webrtc.NewStreamer(func() {
		if s := dev.current(); s != nil {
			if err := s.ResetVideo(); err != nil {
				logger.Warn("key frame request failed", "err", err)
			}
		}
	}, func(msg []byte) {
		s := dev.current()
		if s == nil {
			return
		}
		event, err := control.Decode(msg, &dev.size)
		if err != nil {
			logger.Debug("control message ignored", "err", err)
			return
		}
		if err := s.InjectTouch(event); err != nil {
			logger.Warn("touch injection failed", "err", err)
		}
	}, logger)
	if err != nil {
		return err
	}
	defer streamer.Close()

	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	var workers sync.WaitGroup
	workers.Add(1)
	go func() {
		defer workers.Done()
		supervise.Run(ctx, "video", restartBackoff, logger, func(ctx context.Context) error {
			return runVideo(ctx, cfg, &dev, streamer, logger)
		})
	}()
	if !cfg.noAudio {
		workers.Add(1)
		go func() {
			defer workers.Done()
			supervise.Run(ctx, "audio", restartBackoff, logger, func(ctx context.Context) error {
				return runAudio(ctx, cfg, streamer, logger)
			})
		}()
	}

	handler := signaling.NewHandler(token, streamer, web.Handler())
	servers := make([]*http.Server, 0, 3)
	for _, addr := range listenAddrs(cfg.port, logger) {
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			cancel(fmt.Errorf("listen %s: %w", addr, err))
			break
		}
		srv := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
		servers = append(servers, srv)
		go func() {
			if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				cancel(err)
			}
		}()
		logger.Info("listening", "url", "http://"+addr+"/")
	}
	logger.Info("access token", "file", cfg.tokenFile)

	<-ctx.Done()
	shutdown, done := context.WithTimeout(context.Background(), 2*time.Second)
	defer done()
	for _, srv := range servers {
		_ = srv.Shutdown(shutdown)
	}
	workers.Wait()
	if cause := context.Cause(ctx); cause != nil && !errors.Is(cause, context.Canceled) {
		return cause
	}
	return nil
}

func runVideo(ctx context.Context, cfg config, dev *device, streamer *webrtc.Streamer, logger *slog.Logger) error {
	server, err := scrcpy.Start(ctx, adb.Client{Path: cfg.adbPath, Serial: cfg.serial}, scrcpy.Options{
		ServerPath: cfg.serverPath, MaxSize: cfg.maxSize, VideoBitRate: cfg.videoBitRate, MaxFPS: cfg.maxFPS,
		Control: true,
	}, logger)
	if err != nil {
		return err
	}
	defer server.Close()
	stop := context.AfterFunc(ctx, func() { _ = server.Close() })
	defer stop()

	dev.set(server)
	defer dev.set(nil)
	_ = server.ResetVideo()

	for {
		packet, err := scrcpy.ReadPacket(server.Video)
		if err != nil {
			return fmt.Errorf("video stream ended: %w", err)
		}
		if packet.Session != nil {
			dev.size.Set(uint16(packet.Session.Width), uint16(packet.Session.Height))
			logger.Info("video size", "width", packet.Session.Width, "height", packet.Session.Height)
			continue
		}
		if err := streamer.WriteVideo(packet.Frame); err != nil {
			return err
		}
	}
}

func runAudio(ctx context.Context, cfg config, streamer *webrtc.Streamer, logger *slog.Logger) error {
	tap, err := audiotap.Start(ctx, audiotap.Options{
		AppPath: cfg.audioApp, BundleID: cfg.audioBundle, BitRate: cfg.audioBitRate,
	}, logger)
	if err != nil {
		return err
	}
	defer tap.Close()
	stop := context.AfterFunc(ctx, func() { _ = tap.Close() })
	defer stop()

	for {
		packet, err := tap.ReadPacket()
		if err != nil {
			return fmt.Errorf("audio stream ended: %w", err)
		}
		if err := streamer.WriteAudio(packet.Data); err != nil {
			return err
		}
	}
}

func listenAddrs(port int, logger *slog.Logger) []string {
	p := strconv.Itoa(port)
	addrs := []string{net.JoinHostPort("127.0.0.1", p)}
	tailscale := webrtc.TailscaleAddrs()
	if len(tailscale) == 0 {
		logger.Warn("no Tailscale address found: the iPhone will not reach the bridge until Tailscale is up")
	}
	for _, a := range tailscale {
		addrs = append(addrs, net.JoinHostPort(a.String(), p))
	}
	return addrs
}
