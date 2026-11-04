package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kevindurb/travel-home-lab/travel-tv/internal/tv"
)

var version = "dev"

func main() {
	config := tv.ConfigFromEnv()

	service := tv.NewService(tv.NewWayland(), tv.NewPulseAudio(), config.Settle)
	handler := tv.NewServer(config, service).Handler()

	server := &http.Server{
		Addr:              config.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("travel-tv: shutdown: %v", err)
		}
	}()

	log.Printf("travel-tv %s: listening on %s (www=%s)", version, config.Listen, config.WWW)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("travel-tv: %v", err)
	}
}
