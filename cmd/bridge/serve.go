package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"cloudphone/internal/adb"
	"cloudphone/internal/audiotap"
	"cloudphone/internal/scrcpy"
	"cloudphone/internal/signaling"
	"cloudphone/internal/webrtc"
	"cloudphone/web"
)

func serve(ctx context.Context, cfg config, logger *slog.Logger) error {
	token, err := signaling.LoadOrCreateToken(cfg.tokenFile)
	if err != nil {
		return fmt.Errorf("token: %w", err)
	}

	server, err := scrcpy.Start(ctx, adb.Client{Path: cfg.adbPath, Serial: cfg.serial}, scrcpy.Options{
		ServerPath: cfg.serverPath, MaxSize: cfg.maxSize, VideoBitRate: cfg.videoBitRate, MaxFPS: cfg.maxFPS,
		Control: true,
	}, logger)
	if err != nil {
		return err
	}
	defer server.Close()

	streamer, err := webrtc.NewStreamer(func() {
		if err := server.ResetVideo(); err != nil {
			logger.Warn("key frame request failed", "err", err)
		}
	}, logger)
	if err != nil {
		return err
	}
	defer streamer.Close()

	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	go func() { cancel(pumpVideo(server, streamer)) }()

	if !cfg.noAudio {
		tap, err := audiotap.Start(ctx, audiotap.Options{
			AppPath: cfg.audioApp, BundleID: cfg.audioBundle, BitRate: cfg.audioBitRate,
		}, logger)
		if err != nil {
			return err
		}
		defer tap.Close()
		go func() { cancel(pumpAudio(tap, streamer)) }()
	}

	handler := signaling.NewHandler(token, streamer, web.Handler())
	addrs := listenAddrs(cfg.port, logger)
	servers := make([]*http.Server, 0, len(addrs))
	for _, addr := range addrs {
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("listen %s: %w", addr, err)
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
	if cause := context.Cause(ctx); cause != nil && !errors.Is(cause, context.Canceled) {
		return cause
	}
	return nil
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

func pumpVideo(server *scrcpy.Server, streamer *webrtc.Streamer) error {
	for {
		packet, err := scrcpy.ReadPacket(server.Video)
		if err != nil {
			return fmt.Errorf("video stream ended: %w", err)
		}
		if packet.Frame == nil {
			continue
		}
		if err := streamer.WriteVideo(packet.Frame); err != nil {
			return err
		}
	}
}

func pumpAudio(tap *audiotap.Tap, streamer *webrtc.Streamer) error {
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
