package dockerhost

// connectionKinds are the ways a FreeBSD router reaches Docker. FreeBSD has
// no native Docker, so the daemon is always on another machine.
var connectionKinds = []string{ConnectionSSH, ConnectionTCP}
