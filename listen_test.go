package main

import "testing"

func TestResolveBindHost(t *testing.T) {
	const env = "TEST_3270CONNECT_BIND"

	tests := []struct {
		name string
		flag string
		env  string
		want string
	}{
		{name: "nothing set defaults to localhost", want: defaultBindHost},
		{name: "environment is used when the flag is absent", env: "0.0.0.0", want: "0.0.0.0"},
		{name: "flag wins over the environment", flag: "127.0.0.1", env: "0.0.0.0", want: "127.0.0.1"},
		{name: "blank flag falls through", flag: "   ", env: "10.0.0.5", want: "10.0.0.5"},
		{name: "blank environment falls through", env: "  ", want: defaultBindHost},
		{name: "surrounding space is trimmed", flag: " all ", want: "all"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(env, tc.env)
			if got := resolveBindHost(tc.flag, env); got != tc.want {
				t.Errorf("resolveBindHost(%q, %q) = %q, want %q", tc.flag, tc.env, got, tc.want)
			}
		})
	}
}

func TestListenAddress(t *testing.T) {
	tests := []struct {
		host string
		port int
		want string
	}{
		// Every spelling of "every interface" collapses to a bare ":port", so
		// the listener answers on IPv6 too. 0.0.0.0 alone would not.
		{host: "", port: 9200, want: ":9200"},
		{host: "all", port: 9200, want: ":9200"},
		{host: "any", port: 9200, want: ":9200"},
		{host: "*", port: 9200, want: ":9200"},
		{host: "0.0.0.0", port: 9200, want: ":9200"},
		{host: "::", port: 9200, want: ":9200"},
		{host: "ALL", port: 8080, want: ":8080"},
		{host: " 0.0.0.0 ", port: 8080, want: ":8080"},

		{host: "localhost", port: 9200, want: "localhost:9200"},
		{host: "127.0.0.1", port: 9200, want: "127.0.0.1:9200"},
		{host: "10.0.0.5", port: 8080, want: "10.0.0.5:8080"},
		// A literal IPv6 address has to come back bracketed or net.Listen
		// cannot tell the address from the port.
		{host: "::1", port: 9200, want: "[::1]:9200"},
	}

	for _, tc := range tests {
		if got := listenAddress(tc.host, tc.port); got != tc.want {
			t.Errorf("listenAddress(%q, %d) = %q, want %q", tc.host, tc.port, got, tc.want)
		}
	}
}

func TestDashboardURL(t *testing.T) {
	tests := []struct {
		host string
		port int
		want string
	}{
		// ":9200" is an address, not somewhere to click. Anything meaning
		// "every interface" is browsable at localhost from this machine.
		{host: "", port: 9200, want: "http://localhost:9200/dashboard"},
		{host: "0.0.0.0", port: 9200, want: "http://localhost:9200/dashboard"},
		{host: "all", port: 8500, want: "http://localhost:8500/dashboard"},
		{host: "localhost", port: 9200, want: "http://localhost:9200/dashboard"},
		{host: "10.0.0.5", port: 9200, want: "http://10.0.0.5:9200/dashboard"},
		{host: "::1", port: 9200, want: "http://[::1]:9200/dashboard"},
	}

	for _, tc := range tests {
		if got := dashboardURL(tc.host, tc.port); got != tc.want {
			t.Errorf("dashboardURL(%q, %d) = %q, want %q", tc.host, tc.port, got, tc.want)
		}
	}
}

func TestBindsEveryInterface(t *testing.T) {
	exposed := []string{"", "all", "any", "*", "0.0.0.0", "::", " ALL "}
	for _, host := range exposed {
		if !bindsEveryInterface(host) {
			t.Errorf("bindsEveryInterface(%q) = false, want true", host)
		}
	}

	private := []string{"localhost", "127.0.0.1", "::1", "10.0.0.5"}
	for _, host := range private {
		if bindsEveryInterface(host) {
			t.Errorf("bindsEveryInterface(%q) = true, want false", host)
		}
	}
}
