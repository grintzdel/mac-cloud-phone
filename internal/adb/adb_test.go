package adb

import "testing"

func TestParseForwardPortReadsAllocatedPort(t *testing.T) {
	port, err := parseForwardPort([]byte("43215\n"))
	if err != nil {
		t.Fatal(err)
	}
	if port != 43215 {
		t.Fatalf("got %d", port)
	}
}

func TestParseForwardPortRejectsGarbage(t *testing.T) {
	if _, err := parseForwardPort([]byte("error: no devices\n")); err == nil {
		t.Fatal("expected an error")
	}
}

func TestArgsTargetsTheConfiguredSerial(t *testing.T) {
	c := Client{Path: "adb", Serial: "127.0.0.1:5555"}
	got := c.args("shell", "id")
	want := []string{"-s", "127.0.0.1:5555", "shell", "id"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v", got)
		}
	}
}
