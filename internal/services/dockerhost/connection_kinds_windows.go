package dockerhost

// connectionKinds are the ways a Windows router reaches Docker. Unix sockets
// are not served; Docker in WSL is.
var connectionKinds = []string{ConnectionSSH, ConnectionTCP, ConnectionWSL}

// ConnectionWSL is Docker inside a WSL distro, reached over TCP on the
// loopback.
const ConnectionWSL = "wsl"
