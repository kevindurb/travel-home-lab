package tv

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Wayland injects input by invoking wtype and wlrctl, the same helpers the
// Python remote used. They are bundled into the scratch image.
type Wayland struct {
	runtimeDir string
	timeout    time.Duration
}

// NewWayland builds an injector for the kiosk's Wayland socket.
func NewWayland() *Wayland {
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		runtimeDir = fmt.Sprintf("/run/user/%d", os.Getuid())
	}
	return &Wayland{runtimeDir: runtimeDir, timeout: 4 * time.Second}
}

// Display returns the Wayland display name for the kiosk, or false when Cage
// is not running (for example, no HDMI display is connected).
func (w *Wayland) Display() (string, bool) {
	if override := os.Getenv("WAYLAND_DISPLAY"); override != "" {
		path := override
		if !filepath.IsAbs(path) {
			path = filepath.Join(w.runtimeDir, override)
		}
		if info, err := os.Stat(path); err == nil && info.Mode()&os.ModeSocket != 0 {
			return override, true
		}
	}

	entries, err := os.ReadDir(w.runtimeDir)
	if err != nil {
		return "", false
	}

	type candidate struct {
		name    string
		modTime time.Time
	}
	var candidates []candidate
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "wayland-") {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.Mode()&os.ModeSocket == 0 {
			continue
		}
		candidates = append(candidates, candidate{name: entry.Name(), modTime: info.ModTime()})
	}
	if len(candidates) == 0 {
		return "", false
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].modTime.After(candidates[j].modTime)
	})
	return candidates[0].name, true
}

// HasTools reports whether the bundled helpers can be found on PATH.
func (w *Wayland) HasTools() (bool, bool) {
	_, wtypeErr := exec.LookPath("wtype")
	_, wlrctlErr := exec.LookPath("wlrctl")
	return wtypeErr == nil, wlrctlErr == nil
}

func (w *Wayland) Key(ctx context.Context, name string, repeat int) error {
	if repeat < 1 {
		repeat = 1
	}
	for index := 0; index < repeat; index++ {
		if err := w.run(ctx, "wtype", "-k", name); err != nil {
			return err
		}
		if index+1 < repeat {
			if err := sleepContext(ctx, 80*time.Millisecond); err != nil {
				return err
			}
		}
	}
	return nil
}

func (w *Wayland) Combo(ctx context.Context, mod, key string) error {
	if err := w.run(ctx, "wtype", "-M", mod, "-k", key, "-m", mod); err != nil {
		return err
	}
	return sleepContext(ctx, 150*time.Millisecond)
}

func (w *Wayland) Text(ctx context.Context, text string) error {
	return w.run(ctx, "wtype", "--", text)
}

func (w *Wayland) Pointer(ctx context.Context, dx, dy int) error {
	if dx == 0 && dy == 0 {
		return nil
	}
	return w.run(ctx, "wlrctl", "pointer", "move", strconv.Itoa(dx), strconv.Itoa(dy))
}

func (w *Wayland) Click(ctx context.Context, button string) error {
	args := []string{"pointer", "click"}
	if button != "left" {
		args = append(args, button)
	}
	return w.run(ctx, "wlrctl", args...)
}

func (w *Wayland) Scroll(ctx context.Context, dy, dx int) error {
	if dx == 0 && dy == 0 {
		return nil
	}
	return w.run(ctx, "wlrctl", "pointer", "scroll", strconv.Itoa(dy), strconv.Itoa(dx))
}

func (w *Wayland) run(ctx context.Context, tool string, args ...string) error {
	display, ok := w.Display()
	if !ok {
		return errors.New("TV is offline (no Wayland display)")
	}

	path, err := exec.LookPath(tool)
	if err != nil {
		return fmt.Errorf("%s is not installed", tool)
	}

	runCtx, cancel := context.WithTimeout(ctx, w.timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, path, args...)
	cmd.Env = envWith(os.Environ(), map[string]string{
		"XDG_RUNTIME_DIR": w.runtimeDir,
		"WAYLAND_DISPLAY": display,
		"TMPDIR":          "/tmp",
	})

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	err = cmd.Run()
	if err == nil {
		return nil
	}
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("%s timed out", tool)
	}
	if errors.Is(err, exec.ErrNotFound) {
		return fmt.Errorf("%s is not installed", tool)
	}
	if line := lastLine(output.String()); line != "" {
		return errors.New(line)
	}
	return fmt.Errorf("%s failed", tool)
}

func envWith(base []string, overrides map[string]string) []string {
	result := make([]string, 0, len(base)+len(overrides))
	for _, entry := range base {
		key := entry
		if index := strings.IndexByte(entry, '='); index >= 0 {
			key = entry[:index]
		}
		if _, replaced := overrides[key]; replaced {
			continue
		}
		result = append(result, entry)
	}
	for key, value := range overrides {
		result = append(result, key+"="+value)
	}
	return result
}

func lastLine(output string) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) == 0 {
		return ""
	}
	return strings.TrimSpace(lines[len(lines)-1])
}
