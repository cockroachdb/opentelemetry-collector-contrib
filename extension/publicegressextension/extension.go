// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package publicegressextension // import "github.com/open-telemetry/opentelemetry-collector-contrib/extension/publicegressextension"

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/extension/extensionmiddleware"
)

const (
	defaultDialTimeout = 30 * time.Second
	defaultKeepAlive   = 30 * time.Second
)

var (
	_ component.Component            = (*publicEgressExtension)(nil)
	_ extensionmiddleware.HTTPClient = (*publicEgressExtension)(nil)
	_ http.RoundTripper              = (*publicEgressRoundTripper)(nil)

	blockedAddressPrefixes = []netip.Prefix{
		netip.MustParsePrefix("0.0.0.0/8"),
		netip.MustParsePrefix("100.64.0.0/10"),
		netip.MustParsePrefix("192.0.0.0/24"),
		netip.MustParsePrefix("192.0.2.0/24"),
		netip.MustParsePrefix("192.88.99.0/24"),
		netip.MustParsePrefix("198.18.0.0/15"),
		netip.MustParsePrefix("198.51.100.0/24"),
		netip.MustParsePrefix("203.0.113.0/24"),
		netip.MustParsePrefix("240.0.0.0/4"),
		netip.MustParsePrefix("::/96"),
		netip.MustParsePrefix("64:ff9b::/96"),
		netip.MustParsePrefix("64:ff9b:1::/48"),
		netip.MustParsePrefix("100::/64"),
		netip.MustParsePrefix("2001::/32"),
		netip.MustParsePrefix("2001:2::/48"),
		netip.MustParsePrefix("2001:10::/28"),
		netip.MustParsePrefix("2001:20::/28"),
		netip.MustParsePrefix("2001:db8::/32"),
		netip.MustParsePrefix("2002::/16"),
		netip.MustParsePrefix("3fff::/20"),
		netip.MustParsePrefix("fec0::/10"),
	}
)

type ipResolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

type contextDialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}

type publicEgressExtension struct {
	resolver ipResolver
	dialer   contextDialer
}

func newPublicEgressExtension(
	resolver ipResolver, dialer contextDialer,
) *publicEgressExtension {
	return &publicEgressExtension{resolver: resolver, dialer: dialer}
}

func (*publicEgressExtension) Start(context.Context, component.Host) error {
	return nil
}

func (*publicEgressExtension) Shutdown(context.Context) error {
	return nil
}

// GetHTTPRoundTripper installs the public-only dialer at the transport layer.
// It must be the innermost HTTP middleware so the supplied base is the transport
// created by confighttp rather than another RoundTripper wrapper.
func (e *publicEgressExtension) GetHTTPRoundTripper(
	base http.RoundTripper,
) (http.RoundTripper, error) {
	transport, ok := base.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf(
			"public egress middleware must be the innermost HTTP middleware, got %T", base,
		)
	}
	if transport.TLSClientConfig != nil && transport.TLSClientConfig.InsecureSkipVerify {
		return nil, errors.New("public egress middleware requires TLS certificate verification")
	}

	transport = transport.Clone()
	// A proxy would perform its own destination lookup, bypassing the checked and
	// pinned address selected by dialContext.
	transport.Proxy = nil
	transport.DialContext = e.dialContext
	// Force HTTPS connections through DialContext before the normal TLS handshake.
	transport.DialTLSContext = nil

	return &publicEgressRoundTripper{base: transport}, nil
}

func (e *publicEgressExtension) dialContext(
	ctx context.Context, network, address string,
) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("parsing destination address %q: %w", address, err)
	}

	addresses, err := e.resolve(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("destination %q resolved to no IP addresses", host)
	}

	// Reject the complete DNS result when it contains any non-public address.
	// Filtering only the unsafe entries would conceal split-horizon or rebinding
	// configurations instead of failing closed.
	for _, address := range addresses {
		if !isPublicAddress(address) {
			return nil, fmt.Errorf(
				"destination %q resolved to blocked IP address %s", host, address,
			)
		}
	}

	var dialErrors []error
	for _, resolved := range addresses {
		target := net.JoinHostPort(resolved.Unmap().String(), port)
		conn, dialErr := e.dialer.DialContext(ctx, network, target)
		if dialErr == nil {
			return conn, nil
		}
		dialErrors = append(dialErrors, fmt.Errorf("dialing %s: %w", target, dialErr))
	}
	return nil, errors.Join(dialErrors...)
}

func (e *publicEgressExtension) resolve(
	ctx context.Context, host string,
) ([]netip.Addr, error) {
	if address, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{address}, nil
	}

	addresses, err := e.resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("resolving destination %q: %w", host, err)
	}
	return addresses, nil
}

func isPublicAddress(address netip.Addr) bool {
	address = address.Unmap()
	if !address.IsValid() || !address.IsGlobalUnicast() || address.IsPrivate() ||
		address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() ||
		address.IsMulticast() || address.IsUnspecified() {
		return false
	}
	for _, prefix := range blockedAddressPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

type publicEgressRoundTripper struct {
	base http.RoundTripper
}

func (rt *publicEgressRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL == nil || !strings.EqualFold(req.URL.Scheme, "https") {
		return nil, errors.New("public egress middleware requires an HTTPS destination")
	}
	if req.URL.Hostname() == "" {
		return nil, errors.New("public egress middleware requires a destination hostname")
	}
	if req.URL.User != nil {
		return nil, errors.New("public egress middleware does not allow URL credentials")
	}

	resp, err := rt.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= http.StatusMultipleChoices && resp.StatusCode < http.StatusBadRequest {
		if resp.Body != nil {
			_ = resp.Body.Close()
		}
		return nil, fmt.Errorf(
			"public egress middleware rejected HTTP redirect status %d", resp.StatusCode,
		)
	}
	return resp, nil
}
