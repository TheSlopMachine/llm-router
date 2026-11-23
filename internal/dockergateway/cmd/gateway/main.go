// Command gateway is the program of the router-owned gateway container.
// The router cross-compiles it for linux, embeds it and imports it as an
// image on the Docker host.
//
//	gateway proxy           serve the allow-list proxy (the container command)
//	gateway relay HOST:PORT bridge stdin and stdout to one container port
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/TheSlopMachine/llm-router/internal/dockergateway"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "gateway:", err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	if len(arguments) == 0 {
		return fmt.Errorf("usage: gateway proxy | gateway relay HOST:PORT")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	switch arguments[0] {
	case "proxy":
		token := os.Getenv(dockergateway.EnvironmentProxyToken)
		if token == "" {
			return fmt.Errorf("%s is not set", dockergateway.EnvironmentProxyToken)
		}
		listen := os.Getenv(dockergateway.EnvironmentListen)
		if listen == "" {
			listen = dockergateway.DefaultListen
		}
		hosts := strings.Split(os.Getenv(dockergateway.EnvironmentAllowHosts), ",")
		proxy := dockergateway.NewProxy(hosts, token, os.Stdout)
		return proxy.Serve(ctx, listen)
	case "relay":
		if len(arguments) != 2 {
			return fmt.Errorf("usage: gateway relay HOST:PORT")
		}
		return dockergateway.Relay(ctx, arguments[1], os.Stdin, os.Stdout)
	default:
		return fmt.Errorf("unknown command %q", arguments[0])
	}
}
