package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHelloEndpoint(t *testing.T) {
	t.Parallel()

	server := New(nil, "http://localhost:3000", nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/hello", nil)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	if body := rr.Body.String(); body != "Hello World!!" {
		t.Fatalf("unexpected response body: %q", body)
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Fatalf("expected cors origin header, got %q", got)
	}
}

func TestDevicesEndpointWithoutDB(t *testing.T) {
	t.Parallel()

	server := New(nil, "http://localhost:3000", nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/devices", nil)
	rr := httptest.NewRecorder()

	server.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "DB is not configured") {
		t.Fatalf("unexpected response body: %q", rr.Body.String())
	}
}
