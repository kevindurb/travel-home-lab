package tv

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDisplayFindsNewestWaylandSocket(t *testing.T) {
	dir := t.TempDir()
	old, err := net.Listen("unix", filepath.Join(dir, "wayland-0"))
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	recent, err := net.Listen("unix", filepath.Join(dir, "wayland-1"))
	if err != nil {
		t.Fatal(err)
	}
	defer recent.Close()

	oldPath := filepath.Join(dir, "wayland-0")
	recentPath := filepath.Join(dir, "wayland-1")
	if err := os.Chtimes(oldPath, time.Now().Add(-time.Hour), time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(recentPath, time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}

	t.Setenv("WAYLAND_DISPLAY", "")
	injector := &Wayland{runtimeDir: dir, timeout: time.Second}
	name, ok := injector.Display()
	if !ok || name != "wayland-1" {
		t.Fatalf("Display() = %q, %v", name, ok)
	}
}

func TestDisplayHonoursOverride(t *testing.T) {
	dir := t.TempDir()
	socket, err := net.Listen("unix", filepath.Join(dir, "wayland-7"))
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()

	t.Setenv("WAYLAND_DISPLAY", "wayland-7")
	injector := &Wayland{runtimeDir: dir, timeout: time.Second}
	if name, ok := injector.Display(); !ok || name != "wayland-7" {
		t.Fatalf("Display() = %q, %v", name, ok)
	}
}

func TestDisplayWithoutSocket(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "")
	injector := &Wayland{runtimeDir: t.TempDir(), timeout: time.Second}
	if name, ok := injector.Display(); ok {
		t.Fatalf("Display() = %q, want offline", name)
	}
}
