package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4/pkg/media/oggwriter"

	"cloudphone/internal/adb"
	"cloudphone/internal/audiotap"
	"cloudphone/internal/scrcpy"
)

type config struct {
	logLevel     string
	adbPath      string
	serial       string
	serverPath   string
	maxSize      int
	videoBitRate int
	maxFPS       int
	audioApp     string
	audioBundle  string
	audioBitRate int
	outDir       string
	duration     time.Duration
	noAudio      bool
	port         int
	tokenFile    string
}

func main() {
	commands := map[string]func(context.Context, config, *slog.Logger) error{"capture": capture, "serve": serve}
	if len(os.Args) < 2 || commands[os.Args[1]] == nil {
		fmt.Fprintln(os.Stderr, "usage: bridge capture|serve [flags]")
		os.Exit(2)
	}
	name := os.Args[1]
	cfg := parseFlags(name, os.Args[2:])
	logger := newLogger(cfg.logLevel)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := commands[name](ctx, cfg, logger); err != nil {
		logger.Error(name+" failed", "err", err)
		os.Exit(1)
	}
}

func parseFlags(name string, args []string) config {
	var cfg config
	home, _ := os.UserHomeDir()
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	fs.StringVar(&cfg.logLevel, "log-level", "info", "debug, info, warn or error")
	fs.StringVar(&cfg.adbPath, "adb", "adb", "adb executable")
	fs.StringVar(&cfg.serial, "serial", "127.0.0.1:5555", "adb serial of the Android device")
	fs.StringVar(&cfg.serverPath, "scrcpy-server", "/opt/homebrew/share/scrcpy/scrcpy-server", "scrcpy-server "+scrcpy.ServerVersion)
	fs.IntVar(&cfg.maxSize, "max-size", 0, "limit the longest video dimension (0 = native)")
	fs.IntVar(&cfg.videoBitRate, "video-bit-rate", 8_000_000, "H.264 bit rate")
	fs.IntVar(&cfg.maxFPS, "max-fps", 60, "maximum frame rate")
	fs.StringVar(&cfg.audioApp, "audio-app", "build/CloudPhoneAudio.app", "audio helper app bundle")
	fs.StringVar(&cfg.audioBundle, "audio-bundle", "com.now.gg.BlueStacks", "bundle id of the process to capture")
	fs.IntVar(&cfg.audioBitRate, "audio-bit-rate", 128_000, "Opus bit rate")
	fs.StringVar(&cfg.outDir, "out", "out", "output directory")
	fs.DurationVar(&cfg.duration, "duration", 10*time.Second, "capture duration")
	fs.BoolVar(&cfg.noAudio, "no-audio", false, "disable audio")
	fs.IntVar(&cfg.port, "port", 8080, "HTTP port, bound on localhost and the Tailscale addresses only")
	fs.StringVar(&cfg.tokenFile, "token-file", filepath.Join(home, ".config", "cloudphone", "token"), "access token file")
	_ = fs.Parse(args)
	return cfg
}

func newLogger(level string) *slog.Logger {
	var l slog.Level
	if err := l.UnmarshalText([]byte(level)); err != nil {
		l = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: l}))
}

func capture(ctx context.Context, cfg config, logger *slog.Logger) error {
	if err := os.MkdirAll(cfg.outDir, 0o755); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.duration)
	defer cancel()

	server, err := scrcpy.Start(ctx, adb.Client{Path: cfg.adbPath, Serial: cfg.serial}, scrcpy.Options{
		ServerPath: cfg.serverPath, MaxSize: cfg.maxSize, VideoBitRate: cfg.videoBitRate, MaxFPS: cfg.maxFPS,
	}, logger)
	if err != nil {
		return err
	}
	defer server.Close()

	errs := make(chan error, 2)
	go func() { errs <- writeVideo(server, filepath.Join(cfg.outDir, "video.h264"), logger) }()
	running := 1

	var tap *audiotap.Tap
	if !cfg.noAudio {
		tap, err = audiotap.Start(ctx, audiotap.Options{
			AppPath: cfg.audioApp, BundleID: cfg.audioBundle, BitRate: cfg.audioBitRate,
		}, logger)
		if err != nil {
			return err
		}
		defer tap.Close()
		go func() { errs <- writeAudio(tap, filepath.Join(cfg.outDir, "audio.opus"), logger) }()
		running++
	}

	<-ctx.Done()
	server.Close()
	if tap != nil {
		tap.Close()
	}
	logger.Info("capture finished", "reason", context.Cause(ctx))
	for range running {
		if err := <-errs; err != nil {
			return err
		}
	}
	return nil
}

func writeVideo(server *scrcpy.Server, path string, logger *slog.Logger) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	var frames, bytes int
	defer func() { logger.Info("video written", "path", path, "frames", frames, "bytes", bytes) }()
	for {
		packet, err := scrcpy.ReadPacket(server.Video)
		if err != nil {
			if isClosed(err) {
				return nil
			}
			return fmt.Errorf("read video: %w", err)
		}
		if packet.Session != nil {
			logger.Info("video session", "width", packet.Session.Width, "height", packet.Session.Height)
			continue
		}
		if _, err := f.Write(packet.Frame.Data); err != nil {
			return err
		}
		bytes += len(packet.Frame.Data)
		if !packet.Frame.Config {
			frames++
		}
	}
}

func writeAudio(tap *audiotap.Tap, path string, logger *slog.Logger) error {
	w, err := oggwriter.New(path, 48000, 2)
	if err != nil {
		return err
	}
	defer w.Close()

	var packets int
	defer func() { logger.Info("audio written", "path", path, "packets", packets) }()
	for {
		packet, err := tap.ReadPacket()
		if err != nil {
			if isClosed(err) {
				return nil
			}
			return fmt.Errorf("read audio: %w", err)
		}
		if err := w.WriteRTP(&rtp.Packet{Payload: packet.Data}); err != nil {
			return err
		}
		packets++
	}
}

func isClosed(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed)
}
