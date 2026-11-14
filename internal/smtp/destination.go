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
	return fmt.Sprintf("destination %s is a loopback, private, link-local or unspecified address; refused (set sender.allow_private_destinations to allow it)", e.address)
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

func isPrivateAddr(a netip.Addr) bool {
	a = a.Unmap()
	return a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() ||
		a.IsInterfaceLocalMulticast() || a.IsUnspecified()
}
