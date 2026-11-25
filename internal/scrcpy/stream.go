package scrcpy

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

type Codec uint32

const (
	CodecH264 Codec = 0x68323634
	CodecH265 Codec = 0x68323635
	CodecAV1  Codec = 0x00617631
	CodecOpus Codec = 0x6f707573
)

func (c Codec) String() string {
	switch c {
	case CodecH264:
		return "h264"
	case CodecH265:
		return "h265"
	case CodecAV1:
		return "av1"
	case CodecOpus:
		return "opus"
	}
	return fmt.Sprintf("codec(0x%08x)", uint32(c))
}

const (
	deviceNameLength = 64
	maxPacketSize    = 16 << 20

	flagSession  = uint64(1) << 63
	flagConfig   = uint64(1) << 62
	flagKeyFrame = uint64(1) << 61
	ptsMask      = flagKeyFrame - 1
)

var (
	ErrStreamDisabled = errors.New("scrcpy: stream disabled by the device")
	ErrStreamConfig   = errors.New("scrcpy: stream configuration error on the device")
	ErrPacketTooLarge = errors.New("scrcpy: packet too large")
)

type Session struct {
	Width        uint32
	Height       uint32
	ClientResize bool
}

type Frame struct {
	PTS      int64
	Config   bool
	KeyFrame bool
	Data     []byte
}

type Packet struct {
	Session *Session
	Frame   *Frame
}

func ReadDeviceMeta(r io.Reader) (string, error) {
	buf := make([]byte, deviceNameLength)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", fmt.Errorf("scrcpy: read device meta: %w", err)
	}
	if i := bytes.IndexByte(buf, 0); i >= 0 {
		buf = buf[:i]
	}
	return string(buf), nil
}

func ReadCodec(r io.Reader) (Codec, error) {
	var id uint32
	if err := binary.Read(r, binary.BigEndian, &id); err != nil {
		return 0, fmt.Errorf("scrcpy: read codec: %w", err)
	}
	switch id {
	case 0:
		return 0, ErrStreamDisabled
	case 1:
		return 0, ErrStreamConfig
	}
	return Codec(id), nil
}

func ReadPacket(r io.Reader) (Packet, error) {
	var header [12]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return Packet{}, err
	}
	head := binary.BigEndian.Uint64(header[:8])
	if head&flagSession != 0 {
		return Packet{Session: &Session{
			Width:        binary.BigEndian.Uint32(header[4:8]),
			Height:       binary.BigEndian.Uint32(header[8:12]),
			ClientResize: head&(uint64(1)<<32) != 0,
		}}, nil
	}

	size := binary.BigEndian.Uint32(header[8:12])
	if size > maxPacketSize {
		return Packet{}, fmt.Errorf("%w: %d bytes", ErrPacketTooLarge, size)
	}
	data := make([]byte, size)
	if _, err := io.ReadFull(r, data); err != nil {
		if errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		return Packet{}, err
	}

	frame := &Frame{Data: data}
	if head&flagConfig != 0 {
		frame.Config = true
	} else {
		frame.PTS = int64(head & ptsMask)
		frame.KeyFrame = head&flagKeyFrame != 0
	}
	return Packet{Frame: frame}, nil
}
