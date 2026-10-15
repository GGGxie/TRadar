package ingestion

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"testing"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestSourceTransportRejectsTargetsOutsideFixedHTTPSHost(t *testing.T) {
	called := false
	transport := &sourceTransport{host: "api.github.com", base: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, errors.New("unexpected")
	})}
	for _, target := range []string{
		"http://api.github.com/repos/a/b",
		"https://user:secret@api.github.com/repos/a/b",
		"https://api.github.com:443/repos/a/b",
		"https://example.com/repos/a/b",
	} {
		request, err := http.NewRequest(http.MethodGet, target, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = transport.RoundTrip(request); err == nil || !strings.Contains(err.Error(), "source target rejected") {
			t.Fatalf("target %q err=%v", target, err)
		}
	}
	if called {
		t.Fatal("rejected target reached base transport")
	}
}

func TestPinnedDialRejectsAnyPrivateResolutionBeforeConnecting(t *testing.T) {
	for _, blocked := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "100.64.0.1", "2001:db8::1"} {
		t.Run(blocked, func(t *testing.T) {
			dialed := false
			dial := guardedDialContext(
				"api.github.com",
				func(context.Context, string) ([]net.IPAddr, error) {
					return []net.IPAddr{{IP: net.ParseIP("140.82.121.3")}, {IP: net.ParseIP(blocked)}}, nil
				},
				func(context.Context, string, string) (net.Conn, error) {
					dialed = true
					return nil, errors.New("unexpected")
				},
			)
			_, err := dial(context.Background(), "tcp", "api.github.com:443")
			if err == nil || !strings.Contains(err.Error(), "source address rejected") || dialed {
				t.Fatalf("blocked resolution err=%v dialed=%v", err, dialed)
			}
		})
	}
}

func TestPinnedDialConnectsToResolvedPublicAddress(t *testing.T) {
	var address string
	wantErr := errors.New("dial captured")
	dial := guardedDialContext(
		"api.github.com",
		func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("140.82.121.3")}}, nil
		},
		func(_ context.Context, network, target string) (net.Conn, error) {
			if network != "tcp" {
				t.Fatalf("network=%s", network)
			}
			address = target
			return nil, wantErr
		},
	)
	_, err := dial(context.Background(), "tcp", "api.github.com:443")
	if !errors.Is(err, wantErr) || address != "140.82.121.3:443" {
		t.Fatalf("err=%v address=%q", err, address)
	}
}

func TestRedirectPolicyAllowsOneSameHostHopWithoutCredentials(t *testing.T) {
	policy := sourceRedirectPolicy("api.github.com")
	first, _ := http.NewRequest(http.MethodGet, "https://api.github.com/next", nil)
	first.Header.Set("Authorization", "Bearer secret")
	original, _ := http.NewRequest(http.MethodGet, "https://api.github.com/start", nil)
	if err := policy(first, []*http.Request{original}); err != nil {
		t.Fatalf("same-host redirect: %v", err)
	}
	if first.Header.Get("Authorization") != "" {
		t.Fatal("authorization survived redirect")
	}
	second, _ := http.NewRequest(http.MethodGet, "https://api.github.com/final", nil)
	if err := policy(second, []*http.Request{original, first}); err == nil {
		t.Fatal("second redirect accepted")
	}
	for _, target := range []string{"http://api.github.com/next", "https://example.com/next"} {
		redirected, _ := http.NewRequest(http.MethodGet, target, nil)
		if err := policy(redirected, []*http.Request{original}); err == nil {
			t.Fatalf("unsafe redirect %q accepted", target)
		}
	}
}
