package core

import (
	"net/netip"
	"testing"
)

func TestIsGlobal(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{"8.8.8.8", true},
		{"1.1.1.1", true},
		{"10.0.0.1", false},
		{"127.0.0.1", false},
		{"172.16.0.1", false},
		{"172.32.0.1", true},
		{"192.168.1.1", false},
		{"169.254.1.1", false},
		{"0.0.0.0", false},
		{"255.255.255.255", false},
		{"240.0.0.1", false},
		{"198.18.0.1", false},
		{"203.0.113.5", false},
		{"100.64.0.1", false}, // shared address space: neither private nor global
		{"192.0.0.9", true},   // exception inside 192.0.0.0/24
		{"192.0.0.10", true},
		{"192.0.0.8", false},
		{"2606:4700:4700::1111", true},
		{"::1", false},
		{"::", false},
		{"fe80::1", false},
		{"fc00::1", false},
		{"fd12:3456::1", false},
		{"2001:db8::1", false},
		{"2002::1", false},
		{"2001:4860:4860::8888", true},
		{"2001:1::1", true}, // exception inside 2001::/23
		{"2001:3::1", true},
		{"::ffff:8.8.8.8", true}, // IPv4-mapped follows the IPv4 address
		{"::ffff:10.0.0.1", false},
		{"::ffff:100.64.0.1", false},
		{"64:ff9b::808:808", true}, // NAT64 with public embedded IPv4
		{"64:ff9b::7f00:1", false}, // embedded loopback
		{"64:ff9b::a00:1", false},  // embedded 10.0.0.1
		{"64:ff9b::c0a8:101", false},
		{"64:ff9b::a9fe:a9fe", false}, // embedded link-local 169.254.169.254
		{"64:ff9b::6440:1", false},    // embedded shared 100.64.0.1
		{"64:ff9b:1::1", false},       // local-use NAT64 prefix
		{"fe80::1%eth0", false},       // zone is ignored
		{"2606:4700:4700::1111%eth0", true},
	}
	for _, c := range cases {
		if got := IsGlobal(netip.MustParseAddr(c.addr)); got != c.want {
			t.Errorf("IsGlobal(%s) = %v, want %v", c.addr, got, c.want)
		}
	}
}
