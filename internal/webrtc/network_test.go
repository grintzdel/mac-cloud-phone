package webrtc

import (
	"net"
	"testing"
)

func TestAllowedIPAcceptsOnlyLoopbackAndTailscale(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1":         true,
		"::1":               true,
		"100.101.102.103":   true,
		"fd7a:115c:a1e0::1": true,
		"192.168.1.161":     false,
		"10.0.0.2":          false,
		"100.128.0.1":       false,
		"8.8.8.8":           false,
	}
	for addr, want := range cases {
		if got := allowedIP(net.ParseIP(addr)); got != want {
			t.Errorf("allowedIP(%s) = %v, want %v", addr, got, want)
		}
	}
}
