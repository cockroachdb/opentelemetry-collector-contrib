// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package publicegressextension

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsPublicAddress(t *testing.T) {
	testCases := []struct {
		address string
		public  bool
	}{
		{address: "8.8.8.8", public: true},
		{address: "1.1.1.1", public: true},
		{address: "2606:4700:4700::1111", public: true},
		{address: "0.0.0.1"},
		{address: "10.0.0.1"},
		{address: "100.64.0.1"},
		{address: "127.0.0.1"},
		{address: "169.254.169.254"},
		{address: "172.16.0.1"},
		{address: "192.0.2.1"},
		{address: "192.168.0.1"},
		{address: "198.18.0.1"},
		{address: "203.0.113.1"},
		{address: "224.0.0.1"},
		{address: "240.0.0.1"},
		{address: "::"},
		{address: "::1"},
		{address: "::ffff:10.0.0.1"},
		{address: "64:ff9b::a00:1"},
		{address: "100::1"},
		{address: "2001::1"},
		{address: "2001:db8::1"},
		{address: "2002:a00:1::1"},
		{address: "3fff::1"},
		{address: "fc00::1"},
		{address: "fe80::1"},
		{address: "fec0::1"},
		{address: "ff00::1"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.address, func(t *testing.T) {
			require.Equal(t, testCase.public, isPublicAddress(netip.MustParseAddr(testCase.address)))
		})
	}
}

func TestDialContextPinsValidatedAddress(t *testing.T) {
	resolverCalls := 0
	dialedAddress := ""
	extension := newPublicEgressExtension(
		resolverFunc(func(_ context.Context, network, host string) ([]netip.Addr, error) {
			resolverCalls++
			require.Equal(t, "ip", network)
			require.Equal(t, "collector.example.com", host)
			return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
		}),
		dialerFunc(func(_ context.Context, network, address string) (net.Conn, error) {
			require.Equal(t, "tcp", network)
			dialedAddress = address
			return newPipeConn(t), nil
		}),
	)

	conn, err := extension.dialContext(t.Context(), "tcp", "collector.example.com:443")
	require.NoError(t, err)
	require.NoError(t, conn.Close())
	require.Equal(t, 1, resolverCalls)
	require.Equal(t, "8.8.8.8:443", dialedAddress)
}

func TestDialContextRejectsBlockedDNSAnswers(t *testing.T) {
	testCases := []struct {
		name      string
		addresses []netip.Addr
	}{
		{
			name:      "private",
			addresses: []netip.Addr{netip.MustParseAddr("10.0.0.1")},
		},
		{
			name: "mixed public and private",
			addresses: []netip.Addr{
				netip.MustParseAddr("8.8.8.8"),
				netip.MustParseAddr("10.0.0.1"),
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			dialed := false
			extension := newPublicEgressExtension(
				resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
					return testCase.addresses, nil
				}),
				dialerFunc(func(context.Context, string, string) (net.Conn, error) {
					dialed = true
					return nil, nil
				}),
			)

			_, err := extension.dialContext(t.Context(), "tcp", "collector.example.com:443")
			require.ErrorContains(t, err, "blocked IP address")
			require.False(t, dialed)
		})
	}
}

func TestDialContextRejectsPrivateIPLiteral(t *testing.T) {
	resolverCalled := false
	dialerCalled := false
	extension := newPublicEgressExtension(
		resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
			resolverCalled = true
			return nil, nil
		}),
		dialerFunc(func(context.Context, string, string) (net.Conn, error) {
			dialerCalled = true
			return nil, nil
		}),
	)

	_, err := extension.dialContext(t.Context(), "tcp", "169.254.169.254:443")
	require.ErrorContains(t, err, "blocked IP address")
	require.False(t, resolverCalled)
	require.False(t, dialerCalled)
}

func TestDialContextRevalidatesEveryConnection(t *testing.T) {
	lookup := 0
	extension := newPublicEgressExtension(
		resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
			lookup++
			if lookup == 1 {
				return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
			}
			return []netip.Addr{netip.MustParseAddr("10.0.0.1")}, nil
		}),
		dialerFunc(func(context.Context, string, string) (net.Conn, error) {
			return newPipeConn(t), nil
		}),
	)

	conn, err := extension.dialContext(t.Context(), "tcp", "collector.example.com:443")
	require.NoError(t, err)
	require.NoError(t, conn.Close())

	_, err = extension.dialContext(t.Context(), "tcp", "collector.example.com:443")
	require.ErrorContains(t, err, "blocked IP address")
	require.Equal(t, 2, lookup)
}

func TestHTTPClientRejectsDNSRebinding(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	_, port, err := net.SplitHostPort(server.Listener.Addr().String())
	require.NoError(t, err)

	var lookups atomic.Int32
	var dials atomic.Int32
	extension := newPublicEgressExtension(
		resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
			if lookups.Add(1) == 1 {
				return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
			}
			return []netip.Addr{netip.MustParseAddr("10.0.0.1")}, nil
		}),
		dialerFunc(func(ctx context.Context, network, address string) (net.Conn, error) {
			dials.Add(1)
			expected := net.JoinHostPort("8.8.8.8", port)
			if address != expected {
				return nil, fmt.Errorf("dialed %q, expected validated address %q", address, expected)
			}
			// Route the validated public test address to the local TLS canary. The
			// second request must be rejected before this dialer is reached.
			return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
		}),
	)

	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	transport, err := extension.GetHTTPRoundTripper(&http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
			RootCAs:    roots,
		},
		DisableKeepAlives: true,
	})
	require.NoError(t, err)
	client := &http.Client{Transport: transport}
	endpoint := "https://collector.example.com:" + port + "/v1/logs"

	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader("first payload"))
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.NoError(t, resp.Body.Close())

	req, err = http.NewRequest(http.MethodPost, endpoint, strings.NewReader("second payload"))
	require.NoError(t, err)
	_, err = client.Do(req)
	require.ErrorContains(t, err, "blocked IP address")
	require.Equal(t, int32(2), lookups.Load())
	require.Equal(t, int32(1), dials.Load())
	require.Equal(t, int32(1), requests.Load())
}

func TestDialContextTriesEachValidatedAddress(t *testing.T) {
	var dialed []string
	extension := newPublicEgressExtension(
		resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{
				netip.MustParseAddr("8.8.8.8"),
				netip.MustParseAddr("1.1.1.1"),
			}, nil
		}),
		dialerFunc(func(_ context.Context, _, address string) (net.Conn, error) {
			dialed = append(dialed, address)
			if len(dialed) == 1 {
				return nil, errors.New("first address unavailable")
			}
			return newPipeConn(t), nil
		}),
	)

	conn, err := extension.dialContext(t.Context(), "tcp", "collector.example.com:443")
	require.NoError(t, err)
	require.NoError(t, conn.Close())
	require.Equal(t, []string{"8.8.8.8:443", "1.1.1.1:443"}, dialed)
}

func TestGetHTTPRoundTripperHardensTransport(t *testing.T) {
	base := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialTLSContext: func(context.Context, string, string) (net.Conn, error) {
			return nil, errors.New("must not be used")
		},
	}
	extension := newPublicEgressExtension(net.DefaultResolver, &net.Dialer{})

	roundTripper, err := extension.GetHTTPRoundTripper(base)
	require.NoError(t, err)
	wrapper := roundTripper.(*publicEgressRoundTripper)
	transport := wrapper.base.(*http.Transport)
	require.NotSame(t, base, transport)
	require.Nil(t, transport.Proxy)
	require.NotNil(t, transport.DialContext)
	require.Nil(t, transport.DialTLSContext)
	require.NotNil(t, base.Proxy, "the original transport must not be mutated")
	require.NotNil(t, base.DialTLSContext, "the original transport must not be mutated")
}

func TestGetHTTPRoundTripperRequiresInnermostPosition(t *testing.T) {
	extension := newPublicEgressExtension(net.DefaultResolver, &net.Dialer{})
	_, err := extension.GetHTTPRoundTripper(roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, nil
	}))
	require.ErrorContains(t, err, "must be the innermost HTTP middleware")
}

func TestGetHTTPRoundTripperRequiresTLSVerification(t *testing.T) {
	extension := newPublicEgressExtension(net.DefaultResolver, &net.Dialer{})
	_, err := extension.GetHTTPRoundTripper(&http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	})
	require.ErrorContains(t, err, "requires TLS certificate verification")
}

func TestPublicEgressRoundTripper(t *testing.T) {
	t.Run("allows HTTPS response", func(t *testing.T) {
		wrapper := &publicEgressRoundTripper{base: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(&emptyReader{}),
			}, nil
		})}
		req, err := http.NewRequest(http.MethodPost, "https://collector.example.com/v1/logs", http.NoBody)
		require.NoError(t, err)

		resp, err := wrapper.RoundTrip(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		require.NoError(t, resp.Body.Close())
	})

	t.Run("rejects non HTTPS", func(t *testing.T) {
		called := false
		wrapper := &publicEgressRoundTripper{base: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			called = true
			return nil, nil
		})}
		req, err := http.NewRequest(http.MethodPost, "http://collector.example.com/v1/logs", http.NoBody)
		require.NoError(t, err)

		_, err = wrapper.RoundTrip(req)
		require.ErrorContains(t, err, "requires an HTTPS destination")
		require.False(t, called)
	})

	t.Run("rejects URL credentials", func(t *testing.T) {
		wrapper := &publicEgressRoundTripper{base: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return nil, nil
		})}
		req, err := http.NewRequest(http.MethodPost, "https://user:password@collector.example.com/v1/logs", http.NoBody)
		require.NoError(t, err)

		_, err = wrapper.RoundTrip(req)
		require.ErrorContains(t, err, "does not allow URL credentials")
	})

	t.Run("rejects and closes redirects", func(t *testing.T) {
		body := &trackingReadCloser{}
		wrapper := &publicEgressRoundTripper{base: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusTemporaryRedirect,
				Body:       body,
			}, nil
		})}
		req, err := http.NewRequest(http.MethodPost, "https://collector.example.com/v1/logs", http.NoBody)
		require.NoError(t, err)

		_, err = wrapper.RoundTrip(req)
		require.ErrorContains(t, err, "rejected HTTP redirect")
		require.True(t, body.closed)
	})
}

func TestFactory(t *testing.T) {
	factory := NewFactory()
	require.IsType(t, &Config{}, factory.CreateDefaultConfig())
}

type resolverFunc func(context.Context, string, string) ([]netip.Addr, error)

func (f resolverFunc) LookupNetIP(
	ctx context.Context, network, host string,
) ([]netip.Addr, error) {
	return f(ctx, network, host)
}

type dialerFunc func(context.Context, string, string) (net.Conn, error)

func (f dialerFunc) DialContext(
	ctx context.Context, network, address string,
) (net.Conn, error) {
	return f(ctx, network, address)
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type trackingReadCloser struct {
	closed bool
}

func (*trackingReadCloser) Read([]byte) (int, error) {
	return 0, io.EOF
}

func (r *trackingReadCloser) Close() error {
	r.closed = true
	return nil
}

type emptyReader struct{}

func (*emptyReader) Read([]byte) (int, error) {
	return 0, io.EOF
}

func newPipeConn(t *testing.T) net.Conn {
	client, server := net.Pipe()
	t.Cleanup(func() {
		_ = client.Close()
		_ = server.Close()
	})
	return client
}
