package audiotap

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const maxPacketSize = 64 << 10

var ErrPacketTooLarge = errors.New("audiotap: packet too large")

type Packet struct {
	PTS  int64
	Data []byte
}

func ReadPacket(r io.Reader) (Packet, error) {
	var header [12]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return Packet{}, err
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
	return Packet{PTS: int64(binary.BigEndian.Uint64(header[:8])), Data: data}, nil
}
