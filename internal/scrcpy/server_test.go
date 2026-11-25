package scrcpy

import (
	"slices"
	"testing"
)

func TestServerCommandPinsVersionAndScid(t *testing.T) {
	opts := Options{MaxSize: 1280, VideoBitRate: 8_000_000, MaxFPS: 60}
	got := opts.serverCommand(0x1a2b3c4d)

	wantPrefix := []string{
		"CLASSPATH=/data/local/tmp/scrcpy-server.jar", "app_process", "/", "com.genymobile.scrcpy.Server",
		ServerVersion, "scid=1a2b3c4d",
	}
	if !slices.Equal(got[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("got %v", got)
	}
	for _, arg := range []string{
		"tunnel_forward=true", "video=true", "audio=false", "control=false", "video_codec=h264",
		"max_size=1280", "video_bit_rate=8000000", "max_fps=60", "cleanup=true",
	} {
		if !slices.Contains(got, arg) {
			t.Errorf("missing %q in %v", arg, got)
		}
	}
}

func TestServerCommandEnablesControlWhenAsked(t *testing.T) {
	got := Options{Control: true}.serverCommand(1)
	if !slices.Contains(got, "control=true") {
		t.Fatalf("got %v", got)
	}
	for _, arg := range got {
		if arg == "max_size=0" || arg == "max_fps=0" || arg == "video_bit_rate=0" {
			t.Fatalf("zero-valued option should be omitted: %v", got)
		}
	}
}

func TestSocketNameFormatsScidAsEightHexDigits(t *testing.T) {
	if got := socketName(0xab); got != "localabstract:scrcpy_000000ab" {
		t.Fatalf("got %q", got)
	}
}
