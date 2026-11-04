package tv

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path/filepath"
	"strconv"
	"time"
)

const maxActionBody = 8192

// Server exposes the launcher, the phone remote and the CPR News proxy.
type Server struct {
	config  Config
	service *Service
	cpr     *httputil.ReverseProxy
}

// NewServer builds the HTTP server.
func NewServer(config Config, service *Service) *Server {
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
	return &Server{config: config, service: service, cpr: proxy}
}

// Handler returns the complete HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/action", s.handleAction)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "not found"})
	})
	mux.HandleFunc("/radio/cpr-news", s.handleCPRNews)
	mux.HandleFunc("/r", s.handleRemoteShort)
	mux.Handle("/", http.FileServer(http.Dir(s.config.WWW)))
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
	writeJSON(w, http.StatusOK, s.service.Health(r.Context()))
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
	http.ServeFile(w, r, filepath.Join(s.config.WWW, "remote.html"))
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
