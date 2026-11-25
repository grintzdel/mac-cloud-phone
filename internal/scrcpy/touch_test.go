package scrcpy

import (
	"bytes"
	"os"
	"testing"
)

func TestEncodeTouchMatchesScrcpyWireFormat(t *testing.T) {
	cases := map[string]TouchEvent{
		"touch_down.bin": {Action: TouchDown, PointerID: 3, X: 540, Y: 960, Width: 1080, Height: 1920, Pressure: 1},
		"touch_up.bin":   {Action: TouchUp, PointerID: 3, X: 1079, Y: 1919, Width: 1080, Height: 1920, Pressure: 0},
	}
	for fixture, event := range cases {
		want, err := os.ReadFile("testdata/" + fixture)
		if err != nil {
			t.Fatal(err)
		}
		if got := EncodeTouch(event); !bytes.Equal(got[:], want) {
			t.Errorf("%s:\n got % x\nwant % x", fixture, got, want)
		}
	}
}

func TestEncodeTouchClampsPressure(t *testing.T) {
	got := EncodeTouch(TouchEvent{Action: TouchMove, Pressure: 1.7})
	if got[22] != 0xff || got[23] != 0xff {
		t.Fatalf("pressure above 1 should saturate, got % x", got[22:24])
	}
	got = EncodeTouch(TouchEvent{Action: TouchMove, Pressure: 0.5})
	if got[22] != 0x80 || got[23] != 0x00 {
		t.Fatalf("pressure 0.5 should be 0x8000, got % x", got[22:24])
	}
}

func TestVideoSizeIsUnknownUntilFirstSession(t *testing.T) {
	var size VideoSize
	if _, _, ok := size.Get(); ok {
		t.Fatal("size should be unknown")
	}
	size.Set(1080, 1920)
	if w, h, ok := size.Get(); !ok || w != 1080 || h != 1920 {
		t.Fatalf("got %d %d %v", w, h, ok)
	}
}
