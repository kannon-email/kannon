package smtp

import (
	"context"
	"fmt"
	"net/netip"
	"syscall"
)

// blockedDestinationError reports a dial refused because the address it
// resolved to is not on the public internet.
type blockedDestinationError struct {
	address string
}

func (e *blockedDestinationError) Error() string {
	return fmt.Sprintf("destination %s is a loopback, private, shared, link-local or unspecified address; refused (set sender.allow_private_destinations to allow it)", e.address)
}

// refusePrivateDestinations runs on the address actually being dialed, after
// name resolution, so a name that resolves differently at lookup and at dial
// (DNS rebinding) is judged on what the socket would connect to.
func refusePrivateDestinations(_ context.Context, _, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		return &blockedDestinationError{address: address}
	}
	if isPrivateAddr(ap.Addr()) {
		return &blockedDestinationError{address: address}
	}
	return nil
}

var (
	sharedAddressSpace = netip.MustParsePrefix("100.64.0.0/10")
	nat64WellKnown     = netip.MustParsePrefix("64:ff9b::/96")
	nat64LocalUse      = netip.MustParsePrefix("64:ff9b:1::/48")
	ipv4Compatible     = netip.MustParsePrefix("::/96")
	sixToFour          = netip.MustParsePrefix("2002::/16")
)

func isPrivateAddr(a netip.Addr) bool {
	a = a.Unmap()
	if nat64LocalUse.Contains(a) {
		return true
	}
	if v4, ok := embeddedIPv4(a); ok && isPrivateAddr(v4) {
		return true
	}
	return a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() ||
		a.IsInterfaceLocalMulticast() || a.IsUnspecified() || sharedAddressSpace.Contains(a)
}

// embeddedIPv4 returns the IPv4 address an IPv6 address reaches through
// NAT64 (RFC 6052), the deprecated IPv4-compatible form (RFC 4291) or a 6to4
// tunnel (RFC 3056).
func embeddedIPv4(a netip.Addr) (netip.Addr, bool) {
	b := a.As16()
	switch {
	case !a.Is6():
		return netip.Addr{}, false
	case nat64WellKnown.Contains(a), ipv4Compatible.Contains(a):
		return netip.AddrFrom4([4]byte(b[12:16])), true
	case sixToFour.Contains(a):
		return netip.AddrFrom4([4]byte(b[2:6])), true
	}
	return netip.Addr{}, false
}
