//go:build !windows && !freebsd

package dockerhost

// connectionKinds are the ways a Linux or macOS router reaches Docker. A
// local daemon is reached on its unix socket.
var connectionKinds = []string{ConnectionSSH, ConnectionUnix, ConnectionTCP}
