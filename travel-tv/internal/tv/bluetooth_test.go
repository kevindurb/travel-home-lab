package tv

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeBluetooth struct {
	status   BluetoothStatus
	err      error
	calls    []string
	address  string
	duration time.Duration
}

func (f *fakeBluetooth) Status(context.Context) BluetoothStatus { return f.status }

func (f *fakeBluetooth) Scan(_ context.Context, duration time.Duration) error {
	f.calls = append(f.calls, "scan")
	f.duration = duration
	return f.err
}

func (f *fakeBluetooth) Pair(_ context.Context, address string) error {
	f.calls = append(f.calls, "pair")
	f.address = address
	return f.err
}

func (f *fakeBluetooth) Connect(_ context.Context, address string) error {
	f.calls = append(f.calls, "connect")
	f.address = address
	return f.err
}

func (f *fakeBluetooth) Disconnect(_ context.Context, address string) error {
	f.calls = append(f.calls, "disconnect")
	f.address = address
	return f.err
}

func (f *fakeBluetooth) Forget(_ context.Context, address string) error {
	f.calls = append(f.calls, "forget")
	f.address = address
	return f.err
}

func bluetoothHandler(t *testing.T, bluetooth Bluetooth) http.Handler {
	t.Helper()
	service := newTestService(&fakeInput{}, &fakeAudio{})
	return NewServer(Config{WWW: t.TempDir()}, service, bluetooth, "test").Handler()
}

func TestBluetoothStatus(t *testing.T) {
	fake := &fakeBluetooth{status: BluetoothStatus{
		Available: true,
		Powered:   true,
		Devices:   []Device{{Address: "AA:BB:CC:DD:EE:FF", Name: "Pad", Paired: true, Battery: 80}},
	}}
	handler := bluetoothHandler(t, fake)

	request := httptest.NewRequest(http.MethodGet, "/api/bluetooth", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d %q", response.Code, response.Body.String())
	}

	var status BluetoothStatus
	if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if !status.Available || len(status.Devices) != 1 || status.Devices[0].Name != "Pad" {
		t.Fatalf("status = %+v", status)
	}
}

func TestBluetoothUnavailableByDefault(t *testing.T) {
	handler := bluetoothHandler(t, nil)

	request := httptest.NewRequest(http.MethodGet, "/api/bluetooth", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}

	var status BluetoothStatus
	if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.Available || status.Devices == nil || status.Error == "" {
		t.Fatalf("status = %+v", status)
	}
}

func TestBluetoothActions(t *testing.T) {
	fake := &fakeBluetooth{}
	handler := bluetoothHandler(t, fake)

	post := func(path, body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}

	if response := post("/api/bluetooth/pair", `{"address":"aa:bb:cc:dd:ee:ff"}`); response.Code != http.StatusOK {
		t.Fatalf("pair = %d %q", response.Code, response.Body.String())
	}
	if fake.address != "AA:BB:CC:DD:EE:FF" {
		t.Fatalf("address = %q", fake.address)
	}

	for _, action := range []string{"connect", "disconnect", "forget"} {
		if response := post("/api/bluetooth/"+action, `{"address":"AA:BB:CC:DD:EE:FF"}`); response.Code != http.StatusOK {
			t.Fatalf("%s = %d %q", action, response.Code, response.Body.String())
		}
	}

	if response := post("/api/bluetooth/scan", `{"seconds":30}`); response.Code != http.StatusOK {
		t.Fatalf("scan = %d %q", response.Code, response.Body.String())
	}
	if fake.duration != 30*time.Second {
		t.Fatalf("scan duration = %s", fake.duration)
	}

	if response := post("/api/bluetooth/pair", `{"address":"nope"}`); response.Code != http.StatusBadRequest {
		t.Fatalf("bad address = %d", response.Code)
	}
	if response := post("/api/bluetooth/pair", `not json`); response.Code != http.StatusBadRequest {
		t.Fatalf("bad json = %d", response.Code)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/bluetooth/pair", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("GET pair = %d", response.Code)
	}
}

func TestBluetoothActionFailure(t *testing.T) {
	fake := &fakeBluetooth{err: errors.New("no adapter")}
	handler := bluetoothHandler(t, fake)

	request := httptest.NewRequest(http.MethodPost, "/api/bluetooth/connect",
		strings.NewReader(`{"address":"AA:BB:CC:DD:EE:FF"}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("connect = %d %q", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "no adapter") {
		t.Fatalf("body = %q", response.Body.String())
	}
}

func TestHealthIncludesVersion(t *testing.T) {
	service := newTestService(&fakeInput{}, &fakeAudio{})
	handler := NewServer(Config{WWW: t.TempDir()}, service, nil, "1.2.3").Handler()

	request := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	var health Health
	if err := json.Unmarshal(response.Body.Bytes(), &health); err != nil {
		t.Fatal(err)
	}
	if health.Version != "1.2.3" {
		t.Fatalf("version = %q", health.Version)
	}
}
