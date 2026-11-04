package tv

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testConfig() Config {
	return Config{
		Listen:      ":8080",
		Settle:      time.Second,
		RemoteURL:   "https://travel-tv.example.ts.net/r",
		GridColumns: 4,
		Weather:     WeatherConfig{Units: "fahrenheit"},
		WiFi:        WiFiConfig{SSID: "TravelNet", PSK: "hunter2", Security: "WPA"},
		About: AboutConfig{
			Model:    "Raspberry Pi 5",
			Kernel:   "6.6.0",
			Tailnet:  "example.ts.net",
			Firefox:  "firefox",
			Services: []ServiceLink{{Name: "travel-tv", URL: "https://travel-tv.example.ts.net"}},
		},
		Tiles: []Tile{
			{ID: "jellyfin", Label: "Jellyfin", Icon: "jellyfin", Accent: "#AA5CC3", URL: "https://jellyfin.example.ts.net"},
			{ID: "wifi", Label: "Wi-Fi", Icon: "wifi", Accent: "#FFC46B", Action: "wifi", URL: "wifi.html"},
			{ID: "remote", Label: "Remote", Icon: "phone", Accent: "#5CC8FF", Action: "remote", URL: "remote-qr.html"},
		},
		RadioStations: []RadioStation{
			{ID: "groove-salad", Name: "Groove Salad", Subtitle: "SomaFM", Accent: "#7FB2A6", URL: "https://example.com/stream"},
		},
	}
}

func testServer(t *testing.T, input *fakeInput, audio *fakeAudio) (*Service, http.Handler) {
	t.Helper()
	service := newTestService(input, audio)
	server, err := NewServer(testConfig(), service, nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	return service, server.Handler()
}

func TestServerServesPagesAndShortURL(t *testing.T) {
	_, handler := testServer(t, &fakeInput{}, &fakeAudio{})

	get := func(path string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}

	response := get("/")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `data-page="home"`) {
		t.Fatalf("/ = %d %q", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "Jellyfin") {
		t.Fatalf("/ did not render the tile grid: %q", response.Body.String())
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if got := response.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}

	response = get("/r")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `data-columns="4"`) {
		t.Fatalf("/r = %d %q", response.Code, response.Body.String())
	}

	response = get("/style.css")
	if response.Code != http.StatusOK || !strings.HasPrefix(response.Header().Get("Content-Type"), "text/css") {
		t.Fatalf("/style.css = %d %q", response.Code, response.Header().Get("Content-Type"))
	}

	response = get("/remote-qr.svg")
	if response.Code != http.StatusOK || !strings.HasPrefix(response.Header().Get("Content-Type"), "image/svg+xml") {
		t.Fatalf("/remote-qr.svg = %d %q", response.Code, response.Header().Get("Content-Type"))
	}
	if !strings.Contains(response.Body.String(), "<svg") {
		t.Fatalf("/remote-qr.svg is not SVG: %q", response.Body.String())
	}

	if response := get("/wifi-qr.svg"); response.Code != http.StatusOK {
		t.Fatalf("/wifi-qr.svg = %d", response.Code)
	}

	if response := get("/missing"); response.Code != http.StatusNotFound {
		t.Fatalf("/missing = %d", response.Code)
	}

	response = get("/api/other")
	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), "not found") {
		t.Fatalf("/api/other = %d %q", response.Code, response.Body.String())
	}
}

func TestServerHidesWiFiQRWithoutCredentials(t *testing.T) {
	config := testConfig()
	config.WiFi = WiFiConfig{}
	server, err := NewServer(config, newTestService(&fakeInput{}, &fakeAudio{}), nil, "test")
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/wifi-qr.svg", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("/wifi-qr.svg = %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "/wifi.html", nil)
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Wi-Fi setup pending") {
		t.Fatalf("/wifi.html = %d %q", response.Code, response.Body.String())
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
