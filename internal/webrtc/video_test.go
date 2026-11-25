package webrtc

import (
	"bytes"
	"testing"

	"cloudphone/internal/scrcpy"
)

var (
	spsPps   = []byte{0, 0, 0, 1, 0x67, 0x42, 0xc0, 0x2a, 0, 0, 0, 1, 0x68, 0xce, 0x3c, 0x80}
	idrSlice = []byte{0, 0, 0, 1, 0x65, 0x88, 0x84}
	pSlice   = []byte{0, 0, 0, 1, 0x41, 0x9a}
)

func TestAssemblerHoldsConfigUntilTheNextKeyFrame(t *testing.T) {
	var a frameAssembler
	if _, ok := a.push(&scrcpy.Frame{Config: true, Data: spsPps}); ok {
		t.Fatal("config packet must not be emitted on its own")
	}
	data, ok := a.push(&scrcpy.Frame{KeyFrame: true, Data: idrSlice})
	if !ok || !bytes.Equal(data, append(append([]byte{}, spsPps...), idrSlice...)) {
		t.Fatalf("key frame should carry SPS/PPS, got % x", data)
	}
}

func TestAssemblerRepeatsConfigOnEveryKeyFrame(t *testing.T) {
	var a frameAssembler
	a.push(&scrcpy.Frame{Config: true, Data: spsPps})
	a.push(&scrcpy.Frame{KeyFrame: true, Data: idrSlice})
	data, _ := a.push(&scrcpy.Frame{KeyFrame: true, PTS: 1_000_000, Data: idrSlice})
	if !bytes.HasPrefix(data, spsPps) {
		t.Fatalf("second key frame lost SPS/PPS: % x", data)
	}
}

func TestAssemblerDropsFramesUntilFirstKeyFrame(t *testing.T) {
	var a frameAssembler
	a.push(&scrcpy.Frame{Config: true, Data: spsPps})
	if _, ok := a.push(&scrcpy.Frame{Data: pSlice}); ok {
		t.Fatal("a P frame before any key frame cannot be decoded")
	}
	a.push(&scrcpy.Frame{KeyFrame: true, Data: idrSlice})
	if data, ok := a.push(&scrcpy.Frame{PTS: 16_666, Data: pSlice}); !ok || !bytes.Equal(data, pSlice) {
		t.Fatalf("got %v % x", ok, data)
	}
}

func TestPacketizerStampsPacketsFromDevicePTS(t *testing.T) {
	p := newVideoPacketizer(1200)
	first := p.packetize(idrSlice, 0)
	later := p.packetize(pSlice, 1_000_000)

	if len(first) == 0 || len(later) == 0 {
		t.Fatal("no packets produced")
	}
	if got := later[0].Timestamp - first[0].Timestamp; got != 90_000 {
		t.Fatalf("one second of PTS should be 90000 ticks, got %d", got)
	}
	if !later[len(later)-1].Marker {
		t.Fatal("last packet of a frame must carry the marker bit")
	}
	if later[0].SequenceNumber != first[len(first)-1].SequenceNumber+1 {
		t.Fatal("sequence numbers must be contiguous across frames")
	}
}

func TestPacketizerSplitsLargeFramesBelowMTU(t *testing.T) {
	big := append([]byte{0, 0, 0, 1, 0x65}, bytes.Repeat([]byte{0xab}, 5000)...)
	for _, pkt := range newVideoPacketizer(1200).packetize(big, 0) {
		if size := pkt.MarshalSize(); size > 1200 {
			t.Fatalf("packet of %d bytes exceeds MTU", size)
		}
	}
}
