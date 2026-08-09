package main

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPageBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		host    string
		headers map[string]string
		overTLS bool
		want    string
	}{
		{
			name: "plain http listener",
			host: "localhost:8080",
			want: "http://localhost:8080",
		},
		{
			name:    "direct TLS listener",
			host:    "console.example.com",
			overTLS: true,
			want:    "https://console.example.com",
		},
		{
			// The common reverse-proxy shape: the browser spoke HTTPS to the
			// edge, the hop into this process is plain HTTP. Without the
			// forwarded headers the card would advertise an http:// image on an
			// https:// page, and point at an internal hostname besides.
			name:    "forwarded headers name the browser-facing origin",
			host:    "internal:8080",
			headers: map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-Host": "console.example.com"},
			want:    "https://console.example.com",
		},
		{
			name:    "first entry of a forwarded chain wins",
			host:    "internal:8080",
			headers: map[string]string{"X-Forwarded-Host": "console.example.com, inner.invalid"},
			want:    "http://console.example.com",
		},
		{
			// Not an authority, so there is no URL to build. The template emits
			// no card rather than one a scraper cannot fetch.
			name:    "host with a space yields nothing",
			host:    "internal:8080",
			headers: map[string]string{"X-Forwarded-Host": "evil host"},
			want:    "",
		},
		{
			name: "empty host yields nothing",
			host: "",
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "http://example.invalid/dashboard", nil)
			req.Host = tc.host
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			if tc.overTLS {
				req.TLS = &tls.ConnectionState{}
			}
			if got := pageBaseURL(req); got != tc.want {
				t.Errorf("pageBaseURL() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPageBaseURLNilRequest(t *testing.T) {
	if got := pageBaseURL(nil); got != "" {
		t.Errorf("pageBaseURL(nil) = %q, want empty", got)
	}
}

// The social tags are only worth having if they survive edits to the template,
// so the shape is asserted rather than left to review. The template is embedded,
// so this reads the same bytes the binary serves.
func TestDashboardTemplateSocialTags(t *testing.T) {
	source, err := dashboardTemplateFS.ReadFile("templates/dashboard.gohtml")
	if err != nil {
		t.Fatalf("read embedded dashboard template: %v", err)
	}

	for _, want := range []string{
		`<meta name="description"`,
		`property="og:image"`,
		`property="og:image:width" content="1200"`,
		`property="og:image:height" content="630"`,
		`name="twitter:card" content="summary_large_image"`,
		`{{ if .BaseURL }}`,
	} {
		if !strings.Contains(string(source), want) {
			t.Errorf("dashboard.gohtml: missing %s", want)
		}
	}
}

// The card the tags point at has to be one of the embedded static files, or
// every preview 404s on a deployment that has no filesystem beside the binary.
func TestDashboardOGImageIsEmbedded(t *testing.T) {
	data, err := dashboardTemplateFS.ReadFile("templates/static/images/og-image.png")
	if err != nil {
		t.Fatalf("read embedded og-image.png: %v", err)
	}
	if len(data) == 0 {
		t.Error("embedded og-image.png is empty")
	}
}
