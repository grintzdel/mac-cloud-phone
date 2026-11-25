package scrcpy

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"strconv"
	"time"

	"cloudphone/internal/adb"
)

const (
	ServerVersion = "5.0.1"
	ServerSHA256  = "764eb6f79811d5211fe9df341120882ba9994c7a61b897d7bf3fb662e53bc536"

	deviceServerPath = "/data/local/tmp/scrcpy-server.jar"
	connectTimeout   = 10 * time.Second
)

type Options struct {
	ServerPath   string
	MaxSize      int
	VideoBitRate int
	MaxFPS       int
	Control      bool
}

func (o Options) serverCommand(scid uint32) []string {
	cmd := []string{
		"CLASSPATH=" + deviceServerPath, "app_process", "/", "com.genymobile.scrcpy.Server", ServerVersion,
		fmt.Sprintf("scid=%08x", scid),
		"log_level=info",
		"tunnel_forward=true",
		"video=true",
		"audio=false",
		"control=" + strconv.FormatBool(o.Control),
		"video_codec=h264",
		"cleanup=true",
	}
	for _, opt := range []struct {
		key   string
		value int
	}{{"max_size", o.MaxSize}, {"video_bit_rate", o.VideoBitRate}, {"max_fps", o.MaxFPS}} {
		if opt.value > 0 {
			cmd = append(cmd, opt.key+"="+strconv.Itoa(opt.value))
		}
	}
	return cmd
}

func socketName(scid uint32) string {
	return fmt.Sprintf("localabstract:scrcpy_%08x", scid)
}

type Server struct {
	DeviceName string
	Codec      Codec
	Video      net.Conn
	Control    net.Conn

	adb    adb.Client
	cmd    *exec.Cmd
	port   int
	logger *slog.Logger
}

func Start(ctx context.Context, client adb.Client, opts Options, logger *slog.Logger) (*Server, error) {
	if err := verifyServer(opts.ServerPath); err != nil {
		return nil, err
	}
	if err := client.Connect(ctx); err != nil {
		return nil, err
	}
	if err := client.Push(ctx, opts.ServerPath, deviceServerPath); err != nil {
		return nil, err
	}

	scid, err := randomScid()
	if err != nil {
		return nil, err
	}
	port, err := client.ForwardTCP(ctx, socketName(scid))
	if err != nil {
		return nil, err
	}
	s := &Server{adb: client, port: port, logger: logger}

	s.cmd = client.ShellCommand(context.WithoutCancel(ctx), opts.serverCommand(scid)...)
	stdout, _ := s.cmd.StdoutPipe()
	s.cmd.Stderr = s.cmd.Stdout
	if err := s.cmd.Start(); err != nil {
		s.Close()
		return nil, fmt.Errorf("scrcpy: start server: %w", err)
	}
	go s.logOutput(stdout)
	logger.Info("scrcpy server started", "version", ServerVersion, "scid", fmt.Sprintf("%08x", scid), "port", port)

	if err := s.connect(ctx, opts.Control); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

func (s *Server) connect(ctx context.Context, control bool) error {
	video, err := dialWithDummyByte(ctx, s.port)
	if err != nil {
		return err
	}
	s.Video = video
	if control {
		if s.Control, err = net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(s.port)); err != nil {
			return fmt.Errorf("scrcpy: connect control socket: %w", err)
		}
	}
	if s.DeviceName, err = ReadDeviceMeta(video); err != nil {
		return err
	}
	if s.Codec, err = ReadCodec(video); err != nil {
		return err
	}
	s.logger.Info("scrcpy video stream ready", "device", s.DeviceName, "codec", s.Codec.String())
	return nil
}

func dialWithDummyByte(ctx context.Context, port int) (net.Conn, error) {
	deadline := time.Now().Add(connectTimeout)
	addr := "127.0.0.1:" + strconv.Itoa(port)
	for {
		conn, err := net.Dial("tcp", addr)
		if err == nil {
			var dummy [1]byte
			if _, err = io.ReadFull(conn, dummy[:]); err == nil {
				return conn, nil
			}
			conn.Close()
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("scrcpy: server did not accept connection within %s: %w", connectTimeout, err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (s *Server) logOutput(r io.Reader) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		s.logger.Debug("scrcpy server", "line", scanner.Text())
	}
}

func (s *Server) Close() error {
	var errs []error
	for _, c := range []net.Conn{s.Video, s.Control} {
		if c != nil {
			errs = append(errs, c.Close())
		}
	}
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
		_ = s.cmd.Wait()
	}
	if s.port != 0 {
		errs = append(errs, s.adb.RemoveForward(context.Background(), s.port))
	}
	return errors.Join(errs...)
}

func verifyServer(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("scrcpy: read server: %w", err)
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != ServerSHA256 {
		return fmt.Errorf("scrcpy: %s is not scrcpy-server %s (sha256 %s)", path, ServerVersion, got)
	}
	return nil
}

func randomScid() (uint32, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(b[:]) & 0x7fffffff, nil
}
