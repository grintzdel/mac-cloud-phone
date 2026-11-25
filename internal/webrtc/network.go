package webrtc

import (
	"net"
	"net/netip"
)

var allowedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("fd7a:115c:a1e0::/48"),
}

func allowedIP(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	addr = addr.Unmap()
	for _, p := range allowedPrefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

func TailscaleAddrs() []netip.Addr {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var out []netip.Addr
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		addr, ok := netip.AddrFromSlice(ipnet.IP)
		if ok && !addr.Unmap().IsLoopback() && allowedIP(ipnet.IP) {
			out = append(out, addr.Unmap())
		}
	}
	return out
}
