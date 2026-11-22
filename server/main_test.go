package main

import (
	"net/http/httptest"
	"testing"
)

func TestCheckOrigin(t *testing.T) {
	cases := []struct {
		name, remote, host, origin, fwd string
		ok                              bool
	}{
		{"same host", "100.64.0.2:5000", "100.82.0.1:7788", "http://100.82.0.1:7788", "", true},
		{"mac app forwarder", "100.64.0.2:5000", "127.0.0.1:17788", "http://127.0.0.1:17788", "", true},
		{"no origin (curl)", "100.64.0.2:5000", "100.82.0.1:7788", "", "", true},
		{"other site", "100.64.0.2:5000", "100.82.0.1:7788", "https://evil.example", "", false},
		{"tailscale serve", "127.0.0.1:5000", "127.0.0.1:7788", "https://hub.tail.ts.net", "hub.tail.ts.net", true},
		{"forwarded header from a remote peer is ignored", "100.64.0.2:5000", "100.82.0.1:7788", "https://evil.example", "evil.example", false},
		{"serve but origin differs", "127.0.0.1:5000", "127.0.0.1:7788", "https://evil.example", "hub.tail.ts.net", false},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/ws/events", nil)
		r.RemoteAddr, r.Host = c.remote, c.host
		if c.origin != "" {
			r.Header.Set("Origin", c.origin)
		}
		if c.fwd != "" {
			r.Header.Set("X-Forwarded-Host", c.fwd)
		}
		if got := checkOrigin(r); got != c.ok {
			t.Errorf("%s: got %v, want %v", c.name, got, c.ok)
		}
	}
}
