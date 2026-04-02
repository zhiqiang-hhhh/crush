package tools

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// privateIPNets contains CIDR ranges that should not be accessed by
// fetch/download tools to prevent SSRF attacks.
var privateIPNets = func() []*net.IPNet {
	cidrs := []string{
		"127.0.0.0/8",    // IPv4 loopback
		"10.0.0.0/8",     // RFC 1918
		"172.16.0.0/12",  // RFC 1918
		"192.168.0.0/16", // RFC 1918
		"169.254.0.0/16", // Link-local / cloud metadata
		"0.0.0.0/8",      // "This" network
		"::1/128",        // IPv6 loopback
		"fc00::/7",       // IPv6 unique local
		"fe80::/10",      // IPv6 link-local
	}
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		_, n, err := net.ParseCIDR(cidr)
		if err != nil {
			panic("bad CIDR: " + cidr)
		}
		nets = append(nets, n)
	}
	return nets
}()

// privateHostnames contains hostnames that resolve to private/loopback
// addresses and should be blocked.
var privateHostnames = map[string]struct{}{
	"localhost":                {},
	"metadata.google.internal": {},
}

// IsPrivateURL checks whether a URL targets a private, loopback, or
// link-local address. It is used to prevent SSRF in fetch/download tools.
func IsPrivateURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return true // reject unparseable URLs
	}

	host := u.Hostname()
	if host == "" {
		return true
	}

	// Check against known private hostnames.
	if _, ok := privateHostnames[strings.ToLower(host)]; ok {
		return true
	}

	// Parse as IP (handles both IPv4 and IPv6).
	ip := net.ParseIP(host)
	if ip == nil {
		// Not a raw IP — hostname other than known-private ones is allowed.
		return false
	}

	for _, n := range privateIPNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// SafeTransport returns an *http.Transport that validates resolved IP
// addresses against the private IP list before connecting, preventing
// DNS rebinding attacks.
func SafeTransport() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = 100
	transport.MaxIdleConnsPerHost = 10
	transport.IdleConnTimeout = 90 * time.Second
	transport.DialContext = safeDialContext(transport.DialContext)
	return transport
}

// safeDialContext wraps a DialContext function to reject connections to
// private/loopback IP addresses. This defends against DNS rebinding where
// a hostname passes the IsPrivateURL pre-check but resolves to a private
// IP at connection time.
func safeDialContext(base func(ctx context.Context, network, addr string) (net.Conn, error)) func(ctx context.Context, network, addr string) (net.Conn, error) {
	if base == nil {
		base = (&net.Dialer{Timeout: 30 * time.Second}).DialContext
	}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("SSRF protection: invalid address %q: %w", addr, err)
		}

		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("SSRF protection: DNS lookup failed for %q: %w", host, err)
		}

		for _, ipAddr := range ips {
			for _, n := range privateIPNets {
				if n.Contains(ipAddr.IP) {
					return nil, fmt.Errorf("SSRF protection: resolved IP %s for host %q is in private range %s", ipAddr.IP, host, n)
				}
			}
		}

		return base(ctx, network, net.JoinHostPort(host, port))
	}
}
