package control

import (
	"encoding/json"
	"errors"
	"fmt"

	"cloudphone/internal/scrcpy"
)

const maxPointers = 10

var (
	ErrInvalidMessage = errors.New("control: invalid message")
	ErrNoVideo        = errors.New("control: video size not known yet")
)

type SizeSource interface {
	Get() (width, height uint16, ok bool)
}

type message struct {
	Type    string  `json:"t"`
	Action  string  `json:"a"`
	Pointer int64   `json:"id"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
}

var actions = map[string]scrcpy.TouchAction{
	"down":   scrcpy.TouchDown,
	"up":     scrcpy.TouchUp,
	"move":   scrcpy.TouchMove,
	"cancel": scrcpy.TouchCancel,
}

func Decode(data []byte, size SizeSource) (scrcpy.TouchEvent, error) {
	var m message
	if err := json.Unmarshal(data, &m); err != nil {
		return scrcpy.TouchEvent{}, fmt.Errorf("%w: %v", ErrInvalidMessage, err)
	}
	action, ok := actions[m.Action]
	if m.Type != "touch" || !ok || m.Pointer < 0 || m.Pointer >= maxPointers {
		return scrcpy.TouchEvent{}, fmt.Errorf("%w: %s", ErrInvalidMessage, data)
	}
	w, h, ok := size.Get()
	if !ok {
		return scrcpy.TouchEvent{}, ErrNoVideo
	}

	pressure := float32(1)
	if action == scrcpy.TouchUp || action == scrcpy.TouchCancel {
		pressure = 0
	}
	return scrcpy.TouchEvent{
		Action:    action,
		PointerID: m.Pointer,
		X:         toPixel(m.X, w),
		Y:         toPixel(m.Y, h),
		Width:     w,
		Height:    h,
		Pressure:  pressure,
	}, nil
}

func toPixel(normalized float64, extent uint16) int32 {
	p := int32(normalized * float64(extent))
	return max(0, min(p, int32(extent)-1))
}
