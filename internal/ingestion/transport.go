package ingestion

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

type sourceTransport struct {
	host string
	base http.RoundTripper
}

func (t *sourceTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if err := validateSourceTarget(request.URL, t.host); err != nil {
		return nil, err
	}
	return t.base.RoundTrip(request)
}

func newSourceHTTPClient(source RemoteSource) *http.Client {
	host := sourceAPIHost(source)
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = guardedDialContext(
		host,
		func(ctx context.Context, hostname string) ([]net.IPAddr, error) {
			return net.DefaultResolver.LookupIPAddr(ctx, hostname)
		},
		dialer.DialContext,
	)
	return &http.Client{Transport: &sourceTransport{host: host, base: transport}, CheckRedirect: sourceRedirectPolicy(host)}
}

func sourceAPIHost(source RemoteSource) string {
	switch source {
	case GitHub:
		return "api.github.com"
	case HackerNews:
		return "hacker-news.firebaseio.com"
	case ProductHunt:
		return "api.producthunt.com"
	default:
		return ""
	}
}

func validateSourceTarget(target *url.URL, host string) error {
	if target == nil || host == "" || target.Scheme != "https" || target.User != nil || target.Port() != "" || !strings.EqualFold(target.Hostname(), host) {
		return errors.New("source target rejected")
	}
	return nil
}

func sourceRedirectPolicy(host string) func(*http.Request, []*http.Request) error {
	return func(request *http.Request, previous []*http.Request) error {
		if len(previous) > 1 {
			return errors.New("source redirect limit exceeded")
		}
		if err := validateSourceTarget(request.URL, host); err != nil {
			return err
		}
		request.Header.Del("Authorization")
		return nil
	}
}

type lookupIPAddrFunc func(context.Context, string) ([]net.IPAddr, error)
type dialContextFunc func(context.Context, string, string) (net.Conn, error)

func guardedDialContext(host string, lookup lookupIPAddrFunc, dial dialContextFunc) dialContextFunc {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		requestedHost, port, err := net.SplitHostPort(address)
		if err != nil || !strings.HasPrefix(network, "tcp") || !strings.EqualFold(requestedHost, host) || port != "443" {
			return nil, errors.New("source address rejected")
		}
		addresses, err := lookup(ctx, requestedHost)
		if err != nil {
			return nil, errors.New("source address resolution failed")
		}
		if len(addresses) == 0 {
			return nil, errors.New("source address resolution empty")
		}
		for _, resolved := range addresses {
			if !isPublicSourceIP(resolved.IP) {
				return nil, errors.New("source address rejected")
			}
		}
		return dial(ctx, network, net.JoinHostPort(addresses[0].IP.String(), port))
	}
}

func isPublicSourceIP(ip net.IP) bool {
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsUnspecified() || address.IsMulticast() {
		return false
	}
	for _, prefix := range reservedSourcePrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

var reservedSourcePrefixes = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001:db8::/32"),
}
