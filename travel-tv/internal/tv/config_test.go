package tv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConfigFromEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	raw := `listen: ":9090"
settle_ms: 1200
remote_url: "https://tv.example/r"
grid_columns: 3
weather:
  units: celsius
  latitude: "40.0"
  longitude: "-105.0"
  city: "Boulder"
wifi:
  ssid: "Travel;Net"
  psk: "pa:ss\\word"
  security: WPA
  hidden: true
about:
  model: "Raspberry Pi 5"
  distribution: Debian
  distribution_version: "12"
  kernel: "6.6.0"
  tailnet: example.ts.net
  firefox: firefox
  services:
    - name: travel-tv
      url: https://travel-tv.example.ts.net
tiles:
  - id: jellyfin
    label: Jellyfin
    icon: jellyfin
    accent: "#AA5CC3"
    url: https://jellyfin.example.ts.net
  - id: wifi
    label: Wi-Fi
    icon: wifi
    accent: "#FFC46B"
    url: wifi.html
    action: wifi
radio_stations:
  - id: nts-1
    name: NTS 1
    subtitle: London
    accent: "#E4572E"
    url: https://stream.example/nts
`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TRAVEL_TV_CONFIG", path)

	config, err := ConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if config.Listen != ":9090" || config.Settle != 1200*time.Millisecond {
		t.Fatalf("listen/settle = %q %s", config.Listen, config.Settle)
	}
	if config.GridColumns != 3 || config.Weather.Units != "celsius" {
		t.Fatalf("grid/weather = %d %+v", config.GridColumns, config.Weather)
	}
	if !config.WiFiReady() || config.WiFiPlaceholder() {
		t.Fatalf("wifi ready/placeholder = %t/%t", config.WiFiReady(), config.WiFiPlaceholder())
	}
	if got := config.WiFiPayload(); got != `WIFI:T:WPA;S:Travel\;Net;P:pa\:ss\\word;H:true;;` {
		t.Fatalf("wifi payload = %q", got)
	}
	if len(config.Tiles) != 2 || config.Tiles[1].Action != "wifi" {
		t.Fatalf("tiles = %+v", config.Tiles)
	}
	if len(config.About.Services) != 1 || len(config.RadioStations) != 1 {
		t.Fatalf("about/radio = %+v %+v", config.About, config.RadioStations)
	}
}

func TestConfigDefaultsAndMissingFile(t *testing.T) {
	t.Setenv("TRAVEL_TV_CONFIG", filepath.Join(t.TempDir(), "missing.yml"))
	if _, err := ConfigFromEnv(); err == nil || !strings.Contains(err.Error(), "read config") {
		t.Fatalf("missing config error = %v", err)
	}

	var config Config
	config.applyDefaults()
	if config.Listen != ":8080" || config.GridColumns != 4 {
		t.Fatalf("defaults = %+v", config)
	}
	if config.Settle != 2*time.Second || config.Weather.Units != "fahrenheit" {
		t.Fatalf("defaults = %+v", config)
	}

	clamped := Config{SettleMS: 10}
	clamped.applyDefaults()
	if clamped.Settle != 500*time.Millisecond {
		t.Fatalf("clamped settle = %s", clamped.Settle)
	}
}

func TestWiFiPayloadWithoutCredentials(t *testing.T) {
	config := Config{}
	if config.WiFiReady() {
		t.Fatal("empty credentials reported ready")
	}
	config.WiFi = WiFiConfig{SSID: "REPLACE_ME", PSK: "REPLACE_ME"}
	if !config.WiFiReady() || !config.WiFiPlaceholder() {
		t.Fatalf("placeholder ready/placeholder = %t/%t", config.WiFiReady(), config.WiFiPlaceholder())
	}
}
