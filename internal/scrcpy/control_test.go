package scrcpy

import (
	"bytes"
	"testing"
)

func TestWriteResetVideoSendsSingleTypeByte(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteResetVideo(&buf); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf.Bytes(), []byte{17}) {
		t.Fatalf("got % x", buf.Bytes())
	}
}
