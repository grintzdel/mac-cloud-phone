package scrcpy

import (
	"encoding/binary"
	"sync/atomic"
)

const controlTypeInjectTouch = 2

type TouchAction uint8

const (
	TouchDown   TouchAction = 0
	TouchUp     TouchAction = 1
	TouchMove   TouchAction = 2
	TouchCancel TouchAction = 3
)

type TouchEvent struct {
	Action    TouchAction
	PointerID int64
	X, Y      int32
	Width     uint16
	Height    uint16
	Pressure  float32
}

func EncodeTouch(e TouchEvent) [32]byte {
	var b [32]byte
	b[0] = controlTypeInjectTouch
	b[1] = byte(e.Action)
	binary.BigEndian.PutUint64(b[2:], uint64(e.PointerID))
	binary.BigEndian.PutUint32(b[10:], uint32(e.X))
	binary.BigEndian.PutUint32(b[14:], uint32(e.Y))
	binary.BigEndian.PutUint16(b[18:], e.Width)
	binary.BigEndian.PutUint16(b[20:], e.Height)
	binary.BigEndian.PutUint16(b[22:], pressureFixedPoint(e.Pressure))
	return b
}

func pressureFixedPoint(p float32) uint16 {
	switch {
	case p <= 0:
		return 0
	case p >= 1:
		return 0xffff
	}
	return uint16(p * 65536)
}

func (s *Server) InjectTouch(e TouchEvent) error {
	if s.Control == nil {
		return ErrNoControl
	}
	msg := EncodeTouch(e)
	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	_, err := s.Control.Write(msg[:])
	return err
}

type VideoSize struct {
	packed atomic.Uint32
}

func (v *VideoSize) Set(width, height uint16) {
	v.packed.Store(uint32(width)<<16 | uint32(height))
}

func (v *VideoSize) Get() (uint16, uint16, bool) {
	p := v.packed.Load()
	return uint16(p >> 16), uint16(p), p != 0
}
