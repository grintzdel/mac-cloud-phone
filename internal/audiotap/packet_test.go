package audiotap

import (
	"bytes"
	"errors"
	"io"
	"os"
	"testing"
)

func TestReadPacketDecodesFramedOpusPackets(t *testing.T) {
	data, err := os.ReadFile("testdata/opus_packets.bin")
	if err != nil {
		t.Fatal(err)
	}
	r := bytes.NewReader(data)

	want := []Packet{
		{PTS: 0, Data: []byte{0xfc, 0x01, 0x02}},
		{PTS: 20000, Data: []byte{0xfc, 0xff}},
	}
	for i, w := range want {
		got, err := ReadPacket(r)
		if err != nil {
			t.Fatalf("packet %d: %v", i, err)
		}
		if got.PTS != w.PTS || !bytes.Equal(got.Data, w.Data) {
			t.Fatalf("packet %d: want %+v, got %+v", i, w, got)
		}
	}
	if _, err := ReadPacket(r); !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.EOF, got %v", err)
	}
}

func TestReadPacketRejectsOversizedPacket(t *testing.T) {
	header := []byte{0, 0, 0, 0, 0, 0, 0, 0, 0x00, 0x10, 0x00, 0x00}
	if _, err := ReadPacket(bytes.NewReader(header)); !errors.Is(err, ErrPacketTooLarge) {
		t.Fatalf("got %v", err)
	}
}
