package tv

import (
	"bytes"
	"fmt"
	"html/template"

	"github.com/kevindurb/travel-home-lab/travel-tv/web"
)

// pageNames are the templates rendered once at startup. The names match the
// URLs the kiosk requests.
var pageNames = []string{
	"index.html",
	"forecast.html",
	"wifi.html",
	"radio.html",
	"remote-qr.html",
	"remote.html",
	"settings.html",
	"bluetooth.html",
	"about.html",
}

// assetNames are the static files served next to the rendered pages.
var assetNames = []string{
	"style.css",
	"app.js",
	"bluetooth.js",
	"remote.css",
	"remote.js",
}

// pageData is the template context for every page.
type pageData struct {
	Version       string
	RemoteURL     string
	GridColumns   int
	Weather       WeatherConfig
	WiFi          wifiView
	About         AboutConfig
	Tiles         []Tile
	RadioStations []RadioStation
}

type wifiView struct {
	SSID        string
	Ready       bool
	Placeholder bool
}

func renderPages(config Config, version string) (map[string][]byte, error) {
	templates, err := template.ParseFS(web.FS, "*.html")
	if err != nil {
		return nil, fmt.Errorf("parse page templates: %w", err)
	}

	data := pageData{
		Version:       version,
		RemoteURL:     config.RemoteURL,
		GridColumns:   config.GridColumns,
		Weather:       config.Weather,
		WiFi:          wifiView{SSID: config.WiFi.SSID, Ready: config.WiFiReady(), Placeholder: config.WiFiPlaceholder()},
		About:         config.About,
		Tiles:         config.Tiles,
		RadioStations: config.RadioStations,
	}

	pages := make(map[string][]byte, len(pageNames))
	for _, name := range pageNames {
		var body bytes.Buffer
		if err := templates.ExecuteTemplate(&body, name, data); err != nil {
			return nil, fmt.Errorf("render %s: %w", name, err)
		}
		pages[name] = body.Bytes()
	}
	return pages, nil
}

func loadAssets() (map[string][]byte, error) {
	assets := make(map[string][]byte, len(assetNames))
	for _, name := range assetNames {
		body, err := web.FS.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("load asset %s: %w", name, err)
		}
		assets[name] = body
	}
	return assets, nil
}
