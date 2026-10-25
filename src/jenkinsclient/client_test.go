package jenkinsclient

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewOwnsIndependentTransportWithDefaults(t *testing.T) {
	defaultTransport := http.DefaultTransport.(*http.Transport)
	before := defaultTransport.Clone()

	first := New(Options{})
	second := New(Options{})
	firstTransport, ok := first.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T, want *http.Transport", first.Transport)
	}
	secondTransport, ok := second.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T, want *http.Transport", second.Transport)
	}

	if firstTransport == defaultTransport || secondTransport == defaultTransport {
		t.Fatal("New reused http.DefaultTransport")
	}
	if firstTransport == secondTransport {
		t.Fatal("two clients share one transport")
	}
	if firstTransport.MaxIdleConnsPerHost != 8 {
		t.Errorf("MaxIdleConnsPerHost = %d, want 8", firstTransport.MaxIdleConnsPerHost)
	}
	if firstTransport.MaxIdleConns != 32 {
		t.Errorf("MaxIdleConns = %d, want 32", firstTransport.MaxIdleConns)
	}
	if firstTransport.MaxConnsPerHost != 0 {
		t.Errorf("MaxConnsPerHost = %d, want unlimited", firstTransport.MaxConnsPerHost)
	}
	if first.Timeout != 0 {
		t.Errorf("Client.Timeout = %v, want zero", first.Timeout)
	}
	if defaultTransport.MaxIdleConnsPerHost != before.MaxIdleConnsPerHost ||
		defaultTransport.MaxIdleConns != before.MaxIdleConns ||
		defaultTransport.MaxConnsPerHost != before.MaxConnsPerHost {
		t.Fatal("New mutated http.DefaultTransport")
	}
}

func TestNewHonorsExplicitOptions(t *testing.T) {
	client := New(Options{MaxIdleConnsPerHost: 12, MaxIdleConns: 48})
	transport := client.Transport.(*http.Transport)
	if transport.MaxIdleConnsPerHost != 12 || transport.MaxIdleConns != 48 {
		t.Fatalf("pool options = (%d, %d), want (12, 48)", transport.MaxIdleConnsPerHost, transport.MaxIdleConns)
	}
}

func TestCloseIdleToleratesNilAndOtherTransports(t *testing.T) {
	CloseIdle(nil)
	CloseIdle(&http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, nil
	})})
}

func TestClientReachesServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer server.Close()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	response, err := New(Options{}).Do(request)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(body) != "ok" {
		t.Fatalf("body = %q, want ok", body)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
