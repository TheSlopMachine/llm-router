package shared

import (
	"net"
	"strconv"
)

// PortStatus reports whether port is free to bind on loopback, plus a
// best-effort description of the holder. The actual bind at startup stays
// the source of truth; this exists for a friendlier preflight message.
//
// Both 127.0.0.1 and ::1 get probed: an IPv4-only probe reports free while
// IPv6 loopback stays held.
func PortStatus(port int) (free bool, holder string) {
	for _, addr := range []string{"127.0.0.1", "::1"} {
		ln, err := net.Listen("tcp", net.JoinHostPort(addr, strconv.Itoa(port)))
		if err != nil {
			return false, describePortHolder(port)
		}
		ln.Close()
	}
	return true, ""
}
