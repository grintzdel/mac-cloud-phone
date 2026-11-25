package adb

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

type Client struct {
	Path   string
	Serial string
}

func (c Client) args(args ...string) []string {
	return append([]string{"-s", c.Serial}, args...)
}

func (c Client) run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, c.Path, c.args(args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return out, fmt.Errorf("adb %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

func (c Client) Connect(ctx context.Context) error {
	if !strings.Contains(c.Serial, ":") {
		return nil
	}
	out, err := exec.CommandContext(ctx, c.Path, "connect", c.Serial).CombinedOutput()
	if err != nil {
		return fmt.Errorf("adb connect %s: %w: %s", c.Serial, err, out)
	}
	if !bytes.Contains(out, []byte("connected to")) {
		return fmt.Errorf("adb connect %s: %s", c.Serial, strings.TrimSpace(string(out)))
	}
	return nil
}

func (c Client) Push(ctx context.Context, local, remote string) error {
	_, err := c.run(ctx, "push", local, remote)
	return err
}

func (c Client) Shell(ctx context.Context, command ...string) ([]byte, error) {
	return c.run(ctx, append([]string{"shell"}, command...)...)
}

func (c Client) ShellCommand(ctx context.Context, command ...string) *exec.Cmd {
	return exec.CommandContext(ctx, c.Path, c.args(append([]string{"shell"}, command...)...)...)
}

func (c Client) ForwardTCP(ctx context.Context, remote string) (int, error) {
	out, err := c.run(ctx, "forward", "tcp:0", remote)
	if err != nil {
		return 0, err
	}
	return parseForwardPort(out)
}

func (c Client) RemoveForward(ctx context.Context, port int) error {
	_, err := c.run(ctx, "forward", "--remove", "tcp:"+strconv.Itoa(port))
	return err
}

func parseForwardPort(out []byte) (int, error) {
	port, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil || port <= 0 || port > 65535 {
		return 0, fmt.Errorf("adb forward: unexpected output %q", out)
	}
	return port, nil
}
