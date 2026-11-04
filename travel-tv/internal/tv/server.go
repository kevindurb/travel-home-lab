package tv

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const maxActionBody = 8192

var bluetoothAddressPattern = regexp.MustCompile(`^([0-9A-F]{2}:){5}[0-9A-F]{2}$`)

// Server exposes the launcher, the phone remote, the CPR News proxy and the
// Bluetooth settings API.
type Server struct {
	service   *Service
	bluetooth Bluetooth
	version   string
	cpr       *httputil.ReverseProxy
	pages     map[string][]byte
	assets    map[string][]byte
	qrs       map[string][]byte
}

// NewServer builds the HTTP server. The launcher pages, static assets and QR
// codes are rendered once, at startup, from the embedded web package and the
// Ansible-rendered Config. A nil Bluetooth client is replaced by one that
// reports the feature as unavailable.
func NewServer(config Config, service *Service, bluetooth Bluetooth, version string) (*Server, error) {
	pages, err := renderPages(config, version)
	if err != nil {
		return nil, err
	}
	assets, err := loadAssets()
	if err != nil {
		return nil, err
	}
	qrs := make(map[string][]byte, 2)
	if config.RemoteURL != "" {
		if qrs["remote-qr.svg"], err = qrSVG(config.RemoteURL); err != nil {
			return nil, err
		}
	}
	if config.WiFiReady() {
		if qrs["wifi-qr.svg"], err = qrSVG(config.WiFiPayload()); err != nil {
			return nil, err
		}
	}

	target, _ := url.Parse("http://stream2.cprnetwork.org:8000/cpr1_lo")
	proxy := &httputil.ReverseProxy{
		Director: func(request *http.Request) {
			request.URL.Scheme = target.Scheme
			request.URL.Host = target.Host
			request.URL.Path = target.Path
			request.URL.RawQuery = ""
			request.Host = target.Hostname()
			request.Header.Set("Connection", "")
		},
		FlushInterval: -1,
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			ResponseHeaderTimeout: 10 * time.Second,
			IdleConnTimeout:       90 * time.Second,
		},
	}
	if bluetooth == nil {
		bluetooth = unavailableBluetooth{}
	}
	return &Server{
		service:   service,
		bluetooth: bluetooth,
		version:   version,
		cpr:       proxy,
		pages:     pages,
		assets:    assets,
		qrs:       qrs,
	}, nil
}

// Handler returns the complete HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/action", s.handleAction)
	mux.HandleFunc("/api/bluetooth", s.handleBluetoothStatus)
	for _, action := range []string{"scan", "pair", "connect", "disconnect", "forget"} {
		mux.HandleFunc("/api/bluetooth/"+action, s.handleBluetoothAction(action))
	}
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "not found"})
	})
	mux.HandleFunc("/radio/cpr-news", s.handleCPRNews)
	mux.HandleFunc("/r", s.handleRemoteShort)
	mux.HandleFunc("/wifi-qr.svg", s.handleQR("wifi-qr.svg"))
	mux.HandleFunc("/remote-qr.svg", s.handleQR("remote-qr.svg"))
	mux.HandleFunc("/", s.handleFile)
	return s.headers(mux)
}

func (s *Server) headers(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "not found"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	health := s.service.Health(ctx)
	health.Version = s.version
	writeJSON(w, http.StatusOK, health)
}

// handleBluetoothStatus reports the adapter and every known controller.
func (s *Server) handleBluetoothStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "not found"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	writeJSON(w, http.StatusOK, s.bluetooth.Status(ctx))
}

// handleBluetoothAction runs one BlueZ request. Pairing blocks until the
// controller answers, so the timeout is generous and the page disables the
// button until the response returns.
func (s *Server) handleBluetoothAction(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "not found"})
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxActionBody)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid body size"})
			return
		}

		var payload struct {
			Address string `json:"address"`
			Seconds int    `json:"seconds"`
		}
		if len(body) > 0 {
			if err := json.Unmarshal(body, &payload); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid request"})
				return
			}
		}

		address := strings.ToUpper(strings.TrimSpace(payload.Address))
		if action != "scan" && !bluetoothAddressPattern.MatchString(address) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid Bluetooth address"})
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
		defer cancel()

		switch action {
		case "scan":
			err = s.bluetooth.Scan(ctx, time.Duration(payload.Seconds)*time.Second)
		case "pair":
			err = s.bluetooth.Pair(ctx, address)
		case "connect":
			err = s.bluetooth.Connect(ctx, address)
		case "disconnect":
			err = s.bluetooth.Disconnect(ctx, address)
		case "forget":
			err = s.bluetooth.Forget(ctx, address)
		}
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func (s *Server) handleAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "not found"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxActionBody)
	body, err := io.ReadAll(r.Body)
	if err != nil || len(body) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid body size"})
		return
	}

	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid request"})
		return
	}

	err = s.service.Dispatch(r.Context(), payload)
	if err == nil {
		s.service.ClearError()
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}

	var badRequestError *BadRequestError
	switch {
	case errors.As(err, &badRequestError):
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": badRequestError.Message})
	case errors.Is(err, ErrBusy):
		writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": err.Error()})
	default:
		s.service.RecordError(err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": err.Error()})
	}
}

func (s *Server) handleRemoteShort(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "not found"})
		return
	}
	s.writeBody(w, r, "remote.html", s.pages["remote.html"])
}

// handleFile serves the rendered pages (by name) and the embedded assets.
func (s *Server) handleFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.NotFound(w, r)
		return
	}

	name := strings.TrimPrefix(r.URL.Path, "/")
	if name == "" {
		name = "index.html"
	}
	if body, ok := s.pages[name]; ok {
		s.writeBody(w, r, name, body)
		return
	}
	if body, ok := s.assets[name]; ok {
		s.writeBody(w, r, name, body)
		return
	}
	http.NotFound(w, r)
}

func (s *Server) handleQR(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}
		body, ok := s.qrs[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		s.writeBody(w, r, name, body)
	}
}

func (s *Server) writeBody(w http.ResponseWriter, r *http.Request, name string, body []byte) {
	if body == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType(name))
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

func contentType(name string) string {
	switch {
	case strings.HasSuffix(name, ".html"):
		return "text/html; charset=utf-8"
	case strings.HasSuffix(name, ".css"):
		return "text/css; charset=utf-8"
	case strings.HasSuffix(name, ".js"):
		return "text/javascript; charset=utf-8"
	case strings.HasSuffix(name, ".svg"):
		return "image/svg+xml; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}

func (s *Server) handleCPRNews(w http.ResponseWriter, r *http.Request) {
	s.cpr.ServeHTTP(w, r)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
