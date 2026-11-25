package control

import (
	"errors"
	"testing"

	"cloudphone/internal/scrcpy"
)

type fixedSize struct{ w, h uint16 }

func (s fixedSize) Get() (uint16, uint16, bool) { return s.w, s.h, s.w != 0 }

func TestDecodeMapsNormalizedCoordinatesToVideoPixels(t *testing.T) {
	ev, err := Decode([]byte(`{"t":"touch","a":"down","id":2,"x":0.5,"y":0.25}`), fixedSize{1080, 1920})
	if err != nil {
		t.Fatal(err)
	}
	want := scrcpy.TouchEvent{Action: scrcpy.TouchDown, PointerID: 2, X: 540, Y: 480, Width: 1080, Height: 1920, Pressure: 1}
	if ev != want {
		t.Fatalf("got %+v", ev)
	}
}

func TestDecodeKeepsEdgesInsideTheScreen(t *testing.T) {
	ev, err := Decode([]byte(`{"t":"touch","a":"move","id":0,"x":1.4,"y":-0.2}`), fixedSize{1080, 1920})
	if err != nil {
		t.Fatal(err)
	}
	if ev.X != 1079 || ev.Y != 0 {
		t.Fatalf("got %d,%d", ev.X, ev.Y)
	}
}

func TestDecodeReleasesPressureOnUpAndCancel(t *testing.T) {
	for _, action := range []string{"up", "cancel"} {
		ev, err := Decode([]byte(`{"t":"touch","a":"`+action+`","id":1,"x":0.1,"y":0.1}`), fixedSize{1080, 1920})
		if err != nil {
			t.Fatal(err)
		}
		if ev.Pressure != 0 {
			t.Errorf("%s: pressure %v", action, ev.Pressure)
		}
	}
}

func TestDecodeRejectsUnknownActionsAndPointers(t *testing.T) {
	for _, msg := range []string{
		`{"t":"touch","a":"hover","id":0,"x":0,"y":0}`,
		`{"t":"touch","a":"down","id":10,"x":0,"y":0}`,
		`{"t":"touch","a":"down","id":-1,"x":0,"y":0}`,
		`{"t":"key","a":"down"}`,
		`not json`,
	} {
		if _, err := Decode([]byte(msg), fixedSize{1080, 1920}); !errors.Is(err, ErrInvalidMessage) {
			t.Errorf("%s: got %v", msg, err)
		}
	}
}

func TestDecodeFailsBeforeVideoSizeIsKnown(t *testing.T) {
	_, err := Decode([]byte(`{"t":"touch","a":"down","id":0,"x":0.5,"y":0.5}`), fixedSize{})
	if !errors.Is(err, ErrNoVideo) {
		t.Fatalf("got %v", err)
	}
}
