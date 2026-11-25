package scrcpy

import (
	"bytes"
	"errors"
	"io"
	"os"
	"testing"
)

func openFixture(t *testing.T, name string) io.Reader {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(data)
}

func TestReadDeviceMetaReturnsTrimmedName(t *testing.T) {
	name, err := ReadDeviceMeta(openFixture(t, "device_meta.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if name != "SM-G998B" {
		t.Fatalf("got %q", name)
	}
}

func TestReadCodecRecognizesH264(t *testing.T) {
	codec, err := ReadCodec(openFixture(t, "video_h264.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if codec != CodecH264 || codec.String() != "h264" {
		t.Fatalf("got %v", codec)
	}
}

func TestReadCodecReportsDisabledStream(t *testing.T) {
	_, err := ReadCodec(openFixture(t, "stream_disabled.bin"))
	if !errors.Is(err, ErrStreamDisabled) {
		t.Fatalf("got %v", err)
	}
}

func TestReadCodecReportsDeviceConfigurationError(t *testing.T) {
	_, err := ReadCodec(openFixture(t, "stream_config_error.bin"))
	if !errors.Is(err, ErrStreamConfig) {
		t.Fatalf("got %v", err)
	}
}

func TestReadPacketDecodesSessionsAndFrames(t *testing.T) {
	r := openFixture(t, "video_h264.bin")
	if _, err := ReadCodec(r); err != nil {
		t.Fatal(err)
	}

	want := []Packet{
		{Session: &Session{Width: 1080, Height: 1920}},
		{Frame: &Frame{Config: true, Data: []byte{0, 0, 0, 1, 0x67, 0x42, 0x00, 0x28}}},
		{Frame: &Frame{PTS: 0, KeyFrame: true, Data: []byte{0, 0, 0, 1, 0x65, 0x88}}},
		{Frame: &Frame{PTS: 16666, Data: []byte{0, 0, 0, 1, 0x41}}},
		{Session: &Session{Width: 1920, Height: 1080, ClientResize: true}},
	}
	for i, w := range want {
		got, err := ReadPacket(r)
		if err != nil {
			t.Fatalf("packet %d: %v", i, err)
		}
		assertPacket(t, i, got, w)
	}
	if _, err := ReadPacket(r); !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.EOF at end of stream, got %v", err)
	}
}

func TestReadPacketFailsOnTruncatedPayload(t *testing.T) {
	r := openFixture(t, "video_truncated.bin")
	if _, err := ReadCodec(r); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPacket(r); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("got %v", err)
	}
}

func assertPacket(t *testing.T, i int, got, want Packet) {
	t.Helper()
	switch {
	case want.Session != nil:
		if got.Session == nil || *got.Session != *want.Session {
			t.Fatalf("packet %d: want session %+v, got %+v", i, want.Session, got)
		}
	case want.Frame != nil:
		g := got.Frame
		if g == nil || g.PTS != want.Frame.PTS || g.Config != want.Frame.Config ||
			g.KeyFrame != want.Frame.KeyFrame || !bytes.Equal(g.Data, want.Frame.Data) {
			t.Fatalf("packet %d: want frame %+v, got %+v", i, want.Frame, got.Frame)
		}
	}
}
