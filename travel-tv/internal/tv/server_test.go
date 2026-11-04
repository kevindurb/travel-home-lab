package tv

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testServer(t *testing.T, input *fakeInput, audio *fakeAudio) (*Service, http.Handler) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("launcher"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "remote.html"), []byte("remote"), 0o644); err != nil {
		t.Fatal(err)
	}
	service := newTestService(input, audio)
	server := NewServer(Config{WWW: dir}, service, nil, "test")
	return service, server.Handler()
}

func TestServerServesPagesAndShortURL(t *testing.T) {
	_, handler := testServer(t, &fakeInput{}, &fakeAudio{})

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "launcher" {
		t.Fatalf("/ = %d %q", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if got := response.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}

	request = httptest.NewRequest(http.MethodGet, "/r", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "remote" {
		t.Fatalf("/r = %d %q", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/missing", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("/missing = %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/other", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), "not found") {
		t.Fatalf("/api/other = %d %q", response.Code, response.Body.String())
	}
}

func TestServerHealth(t *testing.T) {
	input := &fakeInput{display: "wayland-0", wtype: true, wlrctl: true}
	audio := &fakeAudio{state: AudioState{Sink: "hdmi", Volume: 0.4}}
	_, handler := testServer(t, input, audio)

	request := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("health = %d", response.Code)
	}
	var health Health
	if err := json.Unmarshal(response.Body.Bytes(), &health); err != nil {
		t.Fatal(err)
	}
	if !health.OK || health.Wayland == nil || *health.Wayland != "wayland-0" {
		t.Fatalf("health = %+v", health)
	}
}

func TestServerActionStatuses(t *testing.T) {
	input := &fakeInput{}
	audio := &fakeAudio{}
	service, handler := testServer(t, input, audio)

	post := func(body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/action", strings.NewReader(body))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}

	if response := post(`{"type":"key","key":"Right"}`); response.Code != http.StatusOK {
		t.Fatalf("ok action = %d %q", response.Code, response.Body.String())
	}
	if response := post(`{"type":"warp"}`); response.Code != http.StatusBadRequest {
		t.Fatalf("bad action = %d", response.Code)
	}
	if response := post(`not json`); response.Code != http.StatusBadRequest {
		t.Fatalf("bad json = %d", response.Code)
	}
	if response := post(strings.Repeat("x", maxActionBody+1)); response.Code != http.StatusBadRequest {
		t.Fatalf("oversized = %d", response.Code)
	}
	if response := post(`[]`); response.Code != http.StatusBadRequest {
		t.Fatalf("array = %d", response.Code)
	}

	service.keyLock.Lock()
	response := post(`{"type":"launch","index":1,"columns":4}`)
	service.keyLock.Unlock()
	if response.Code != http.StatusConflict {
		t.Fatalf("busy = %d %q", response.Code, response.Body.String())
	}

	request := httptest.NewRequest(http.MethodGet, "/api/action", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("GET action = %d", response.Code)
	}
}

func TestServerHelperFailureIsReported(t *testing.T) {
	input := &fakeInput{display: "wayland-0", wtype: true, wlrctl: true, err: errors.New("wtype failed")}
	audio := &fakeAudio{}
	_, handler := testServer(t, input, audio)

	request := httptest.NewRequest(http.MethodPost, "/api/action", strings.NewReader(`{"type":"key","key":"Right"}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("action = %d %q", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/api/health", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var health Health
	if err := json.Unmarshal(response.Body.Bytes(), &health); err != nil {
		t.Fatal(err)
	}
	if health.LastError == nil || *health.LastError != "wtype failed" {
		t.Fatalf("health = %+v", health)
	}
}
