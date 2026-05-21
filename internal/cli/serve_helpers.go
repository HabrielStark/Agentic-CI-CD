package cli

import "net"

// newListener is a tiny shim used by tests to grab an arbitrary free port
// without racing on the OS-level port table.
func newListener(addr string) (net.Listener, error) {
	return net.Listen("tcp", addr)
}
