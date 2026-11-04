package tv

import (
	"os"
	"strconv"
	"time"
)

// Config is the runtime configuration of the Travel TV server.
type Config struct {
	// Listen is the TCP address inside the container, e.g. ":8080".
	Listen string
	// WWW is the directory holding the Ansible-rendered launcher pages.
	WWW string
	// Settle is how long to wait after Alt+Home before walking the app grid.
	Settle time.Duration
}

// ConfigFromEnv reads the container environment. Defaults match the quadlet.
func ConfigFromEnv() Config {
	return Config{
		Listen: envString("TRAVEL_TV_LISTEN", ":8080"),
		WWW:    envString("TRAVEL_TV_WWW", "/usr/share/travel-tv/www"),
		Settle: settleFromEnv(),
	}
}

func envString(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func settleFromEnv() time.Duration {
	const defaultSettle = 2 * time.Second
	raw := os.Getenv("TRAVEL_TV_REMOTE_SETTLE_MS")
	if raw == "" {
		return defaultSettle
	}
	ms, err := strconv.Atoi(raw)
	if err != nil {
		return defaultSettle
	}
	settle := time.Duration(ms) * time.Millisecond
	if settle < 500*time.Millisecond {
		return 500 * time.Millisecond
	}
	return settle
}
