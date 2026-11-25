package webrtc

import (
	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"

	"cloudphone/internal/scrcpy"
)

const videoClockRate = 90_000

type frameAssembler struct {
	config      []byte
	hadKeyFrame bool
}

func (a *frameAssembler) push(f *scrcpy.Frame) ([]byte, bool) {
	if f.Config {
		a.config = append([]byte(nil), f.Data...)
		return nil, false
	}
	if f.KeyFrame {
		a.hadKeyFrame = true
		return append(append([]byte(nil), a.config...), f.Data...), true
	}
	if !a.hadKeyFrame {
		return nil, false
	}
	return f.Data, true
}

func (a *frameAssembler) reset() {
	a.hadKeyFrame = false
}

type videoPacketizer struct {
	packetizer rtp.Packetizer
	base       uint32
}

func newVideoPacketizer(mtu uint16) *videoPacketizer {
	return &videoPacketizer{
		packetizer: rtp.NewPacketizer(mtu, 0, 0, &codecs.H264Payloader{}, rtp.NewRandomSequencer(), videoClockRate),
	}
}

func (p *videoPacketizer) packetize(data []byte, ptsMicros int64) []*rtp.Packet {
	packets := p.packetizer.Packetize(data, 0)
	ts := p.base + uint32(ptsMicros*videoClockRate/1_000_000)
	for _, pkt := range packets {
		pkt.Timestamp = ts
	}
	return packets
}
