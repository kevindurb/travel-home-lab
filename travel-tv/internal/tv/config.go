package tv

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the runtime configuration of the Travel TV server. Everything the
// pages need is rendered by Ansible into one YAML file (TRAVEL_TV_CONFIG) and
// mounted into the container; the markup itself is embedded in the image.
type Config struct {
	// Listen is the TCP address inside the container, e.g. ":8080".
	Listen string `yaml:"listen"`
	// SettleMS is how long to wait after Alt+Home before walking the app grid.
	SettleMS int `yaml:"settle_ms"`
	// Settle is SettleMS as a duration, filled in by ConfigFromEnv.
	Settle time.Duration `yaml:"-"`
	// RemoteURL is encoded in the phone-remote QR code.
	RemoteURL string `yaml:"remote_url"`
	// GridColumns is the launcher grid width. The phone remote uses it to turn
	// a tile index into arrow-key presses.
	GridColumns int `yaml:"grid_columns"`

	Weather       WeatherConfig  `yaml:"weather"`
	WiFi          WiFiConfig     `yaml:"wifi"`
	About         AboutConfig    `yaml:"about"`
	Tiles         []Tile         `yaml:"tiles"`
	RadioStations []RadioStation `yaml:"radio_stations"`
}

// WeatherConfig carries the launcher/forecast page location settings.
type WeatherConfig struct {
	Units     string `yaml:"units"`
	Latitude  string `yaml:"latitude"`
	Longitude string `yaml:"longitude"`
	City      string `yaml:"city"`
}

// WiFiConfig carries the join credentials for the Wi-Fi QR tile. The PSK is
// rendered by Ansible from the vault and never leaves the process.
type WiFiConfig struct {
	SSID     string `yaml:"ssid"`
	PSK      string `yaml:"psk"`
	Security string `yaml:"security"`
	Hidden   bool   `yaml:"hidden"`
}

// AboutConfig is the static host information shown on the About page.
type AboutConfig struct {
	Model               string        `yaml:"model"`
	Distribution        string        `yaml:"distribution"`
	DistributionVersion string        `yaml:"distribution_version"`
	Kernel              string        `yaml:"kernel"`
	Tailnet             string        `yaml:"tailnet"`
	Firefox             string        `yaml:"firefox"`
	Services            []ServiceLink `yaml:"services"`
}

// ServiceLink is one Tailscale Service listed on the About page.
type ServiceLink struct {
	Name string `yaml:"name"`
	URL  string `yaml:"url"`
}

// Tile is one launcher tile, in grid order.
type Tile struct {
	ID     string `yaml:"id"`
	Label  string `yaml:"label"`
	Icon   string `yaml:"icon"`
	Accent string `yaml:"accent"`
	URL    string `yaml:"url"`
	Action string `yaml:"action"`
}

// RadioStation is one station on the Radio page.
type RadioStation struct {
	ID       string `yaml:"id"`
	Name     string `yaml:"name"`
	Subtitle string `yaml:"subtitle"`
	Accent   string `yaml:"accent"`
	URL      string `yaml:"url"`
}

// ConfigFromEnv reads the Ansible-rendered YAML file named by TRAVEL_TV_CONFIG.
func ConfigFromEnv() (Config, error) {
	path := envString("TRAVEL_TV_CONFIG", "/etc/travel-tv/config.yml")
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(raw, &config); err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	config.applyDefaults()
	if err := config.validate(); err != nil {
		return Config{}, fmt.Errorf("config %s: %w", path, err)
	}
	return config, nil
}

func (c *Config) applyDefaults() {
	if c.Listen == "" {
		c.Listen = ":8080"
	}
	if c.SettleMS == 0 {
		c.SettleMS = 2000
	}
	c.Settle = settleFromMS(c.SettleMS)
	if c.GridColumns == 0 {
		c.GridColumns = 4
	}
	if c.Weather.Units == "" {
		c.Weather.Units = "fahrenheit"
	}
	if c.WiFi.Security == "" {
		c.WiFi.Security = "WPA"
	}
}

func (c Config) validate() error {
	if c.GridColumns < 1 {
		return fmt.Errorf("grid_columns must be positive")
	}
	if c.Listen == "" {
		return fmt.Errorf("listen must not be empty")
	}
	if c.RemoteURL == "" {
		return fmt.Errorf("remote_url must not be empty")
	}
	return nil
}

// WiFiReady reports whether both credentials are set.
func (c Config) WiFiReady() bool {
	return c.WiFi.SSID != "" && c.WiFi.PSK != ""
}

// WiFiPlaceholder reports whether the checked-in placeholder credentials are
// still in place.
func (c Config) WiFiPlaceholder() bool {
	return c.WiFi.SSID == "REPLACE_ME" || c.WiFi.PSK == "REPLACE_ME"
}

// WiFiPayload returns the ZXing WIFI: payload encoded in the Wi-Fi QR code.
func (c Config) WiFiPayload() string {
	hidden := "false"
	if c.WiFi.Hidden {
		hidden = "true"
	}
	security := c.WiFi.Security
	if security == "" {
		security = "WPA"
	}
	return "WIFI:T:" + security + ";S:" + wifiEscape(c.WiFi.SSID) +
		";P:" + wifiEscape(c.WiFi.PSK) + ";H:" + hidden + ";;"
}

func wifiEscape(value string) string {
	var out []rune
	for _, char := range value {
		switch char {
		case '\\', ';', ',', ':', '"':
			out = append(out, '\\', char)
		default:
			out = append(out, char)
		}
	}
	return string(out)
}

func envString(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func settleFromMS(ms int) time.Duration {
	settle := time.Duration(ms) * time.Millisecond
	if settle < 500*time.Millisecond {
		return 500 * time.Millisecond
	}
	return settle
}
