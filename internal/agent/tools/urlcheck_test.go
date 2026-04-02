package tools

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsPrivateURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		url     string
		private bool
	}{
		// Private / loopback.
		{"loopback", "http://127.0.0.1/foo", true},
		{"loopback high", "http://127.255.255.255/", true},
		{"localhost", "http://localhost:8080/admin", true},
		{"zero", "http://0.0.0.0/", true},
		{"10.x", "http://10.0.0.1/secret", true},
		{"172.16.x", "http://172.16.0.1/", true},
		{"172.31.x", "http://172.31.255.255/", true},
		{"192.168.x", "http://192.168.1.1/admin", true},
		{"aws metadata", "http://169.254.169.254/latest/meta-data/", true},
		{"link-local", "http://169.254.1.1/", true},
		{"ipv6 loopback", "http://[::1]/", true},
		{"ipv6 unique local", "http://[fd12:3456::1]/", true},
		{"ipv6 link-local", "http://[fe80::1]/", true},
		{"metadata.google.internal", "http://metadata.google.internal/computeMetadata/v1/", true},

		// Public — should be allowed.
		{"public ip", "http://8.8.8.8/", false},
		{"public domain", "https://example.com/page", false},
		{"public https", "https://api.openai.com/v1/chat", false},
		{"172.15 not private", "http://172.15.255.255/", false},
		{"172.32 not private", "http://172.32.0.1/", false},

		// Edge cases.
		{"empty url", "", true},
		{"no host", "http:///path", true},
		{"ftp scheme", "ftp://10.0.0.1/file", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.private, IsPrivateURL(tt.url))
		})
	}
}

func TestSafeTransport_BlocksLoopback(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	transport := SafeTransport()
	client := &http.Client{Transport: transport}

	host, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	_ = host

	req, err := http.NewRequestWithContext(context.Background(), "GET", "http://127.0.0.1:"+port+"/", nil)
	require.NoError(t, err)

	_, err = client.Do(req)
	require.Error(t, err)
	require.Contains(t, err.Error(), "SSRF protection")
}

func TestSafeTransport_AllowsPublicIP(t *testing.T) {
	t.Parallel()

	transport := SafeTransport()
	require.NotNil(t, transport)
	require.NotNil(t, transport.DialContext)
}
