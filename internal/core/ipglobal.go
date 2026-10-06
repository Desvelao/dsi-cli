package core

import "net/netip"

func prefixes(cidrs ...string) []netip.Prefix {
	out := make([]netip.Prefix, len(cidrs))
	for i, c := range cidrs {
		out[i] = netip.MustParsePrefix(c)
	}
	return out
}

// Address ranges as defined by Python 3.12's ipaddress module (IANA special
// purpose registries); IsGlobal reproduces ipaddress.ip_address(x).is_global.
var (
	v4Private    = prefixes("0.0.0.0/8", "10.0.0.0/8", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24", "192.0.0.170/31", "192.0.2.0/24", "192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "255.255.255.255/32")
	v4Exceptions = prefixes("192.0.0.9/32", "192.0.0.10/32")
	v4Shared     = netip.MustParsePrefix("100.64.0.0/10")
	v6Private    = prefixes("::1/128", "::/128", "64:ff9b:1::/48", "100::/64", "2001::/23", "2001:db8::/32", "2002::/16", "3fff::/20", "fc00::/7", "fe80::/10")
	v6Exceptions = prefixes("2001:1::1/128", "2001:1::2/128", "2001:3::/32", "2001:4:112::/48", "2001:20::/28", "2001:30::/28")
)

func inAny(a netip.Addr, nets []netip.Prefix) bool {
	for _, n := range nets {
		if n.Contains(a) {
			return true
		}
	}
	return false
}

func isPrivate(a netip.Addr, private, exceptions []netip.Prefix) bool {
	return inAny(a, private) && !inAny(a, exceptions)
}

// IsGlobal reports whether the address is globally routable, with the semantics
// of Python's ipaddress.is_global (IPv4-mapped IPv6 addresses follow their IPv4
// address; 100.64.0.0/10 is neither private nor global).
func IsGlobal(a netip.Addr) bool {
	a = a.WithZone("")
	if a.Is4In6() {
		return IsGlobal(a.Unmap())
	}
	if a.Is4() {
		return !v4Shared.Contains(a) && !isPrivate(a, v4Private, v4Exceptions)
	}
	return !isPrivate(a, v6Private, v6Exceptions)
}
