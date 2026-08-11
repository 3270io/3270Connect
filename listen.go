package main

// Where the dashboard and the REST API listen.
//
// Both have always bound localhost and that stays the default. The console has
// no sign-in, and /start-process launches a load run for anybody who can reach
// it, so a listener on every interface is a decision somebody takes rather than
// one they inherit by upgrading.
//
// A container is the deployment that has to take it. A published port forwards
// to the container's external interface, so a loopback listener inside one
// refuses every connection from the host while the container still reports
// healthy — the port mapping looks right, the page never loads, and nothing
// says why. The image sets DASHBOARD_BIND=0.0.0.0 for exactly that reason, and
// what the console is exposed to is then decided by the port mapping in
// docker-compose.yml.

import (
	"flag"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

// The interface both listeners bind when nothing says otherwise.
const defaultBindHost = "localhost"

// Environment variables read when the corresponding flag is not given. They
// exist for containers, where the command line belongs to whoever wrote the
// image's CMD and the operator only gets to set environment.
const (
	dashboardBindEnv = "DASHBOARD_BIND"
	apiBindEnv       = "API_BIND"
)

var (
	// Empty rather than "localhost" so an unset flag can be told from one
	// passed the default value: only the first should fall through to the
	// environment.
	dashboardBind string
	apiBind       string
)

func init() {
	flag.StringVar(&dashboardBind, "dashboardBind", "",
		"Interface for the dashboard listener: localhost (default), an address, or 'all' for every interface. Overrides "+dashboardBindEnv+".")
	flag.StringVar(&apiBind, "api-bind", "",
		"Interface for the API listener: localhost (default), an address, or 'all' for every interface. Overrides "+apiBindEnv+".")
}

// resolveBindHost picks the interface to bind: the flag, then the environment
// variable, then localhost.
func resolveBindHost(flagValue, envName string) string {
	if host := strings.TrimSpace(flagValue); host != "" {
		return host
	}
	if host := strings.TrimSpace(os.Getenv(envName)); host != "" {
		return host
	}
	return defaultBindHost
}

// listenAddress turns a bind host and port into an address for net.Listen.
//
// "all", "any", "*" and 0.0.0.0 all collapse to a bare ":port", which listens
// on every interface on both IP families — 0.0.0.0 alone would be IPv4 only,
// and a host that reaches the container over IPv6 would find nothing there.
func listenAddress(host string, port int) string {
	host = strings.TrimSpace(host)
	switch strings.ToLower(host) {
	case "", "all", "any", "*", "0.0.0.0", "::":
		host = ""
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

// browsableHost is the host to put in a URL printed for somebody to open.
//
// A bare ":9200" is an address, not somewhere to click, and neither is
// 0.0.0.0. Both mean "every interface", which from the machine running the
// process includes localhost.
func browsableHost(host string) string {
	host = strings.TrimSpace(host)
	switch strings.ToLower(host) {
	case "", "all", "any", "*", "0.0.0.0", "::":
		return "localhost"
	}
	return host
}

// dashboardURL is the address to open the console at, given where it bound.
func dashboardURL(host string, port int) string {
	return fmt.Sprintf("http://%s/dashboard", net.JoinHostPort(browsableHost(host), strconv.Itoa(port)))
}

// bindsEveryInterface reports whether this host leaves the listener reachable
// from the network, which is worth saying out loud when it happens.
func bindsEveryInterface(host string) bool {
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "", "all", "any", "*", "0.0.0.0", "::":
		return true
	}
	return false
}
