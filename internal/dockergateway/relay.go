package dockergateway

import (
	"context"
	"fmt"
	"io"
	"net"
)

// Relay connects to address and bridges input and output to it. It returns
// when the remote side closes. The end of input half-closes the connection
// so the remote side sees the end of the request. The process exits after
// Relay returns, which ends the input copy.
func Relay(ctx context.Context, address string, input io.Reader, output io.Writer) error {
	dialer := net.Dialer{Timeout: dialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("relay dial %s: %w", address, err)
	}
	defer conn.Close()
	go func() {
		_, _ = io.Copy(conn, input)
		if tcp, ok := conn.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		}
	}()
	_, err = io.Copy(output, conn)
	return err
}
