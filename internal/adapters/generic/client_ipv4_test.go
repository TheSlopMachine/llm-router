package generic

import (
	"context"
	"net"
	"testing"
)

func TestNewIPv4TransportAlwaysDialsTCP4(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on IPv4 loopback: %v", err)
	}
	defer listener.Close()

	transport := newIPv4Transport()
	defer transport.CloseIdleConnections()

	// Deliberately pass tcp6: the provider transport must override the
	// caller's network family and establish an IPv4 connection.
	connection, err := transport.DialContext(context.Background(), "tcp6", listener.Addr().String())
	if err != nil {
		t.Fatalf("dial IPv4 listener through IPv4-only transport: %v", err)
	}
	defer connection.Close()

	localAddress, ok := connection.LocalAddr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("local address has type %T, want *net.TCPAddr", connection.LocalAddr())
	}
	if localAddress.IP.To4() == nil {
		t.Fatalf("connection used non-IPv4 local address %v", localAddress.IP)
	}
}
