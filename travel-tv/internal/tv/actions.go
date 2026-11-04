package tv

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"
)

var allowedKeys = map[string]struct{}{
	"Up": {}, "Down": {}, "Left": {}, "Right": {},
	"Return": {}, "Escape": {}, "BackSpace": {}, "space": {}, "Tab": {},
	"Home": {}, "End": {}, "Page_Up": {}, "Page_Down": {},
}

var allowedModifiers = map[string]struct{}{
	"alt": {}, "ctrl": {}, "shift": {}, "logo": {},
}

var allowedButtons = map[string]struct{}{
	"left": {}, "right": {}, "middle": {},
}

// Input injects events into the kiosk's Wayland compositor.
type Input interface {
	Display() (name string, ok bool)
	HasTools() (wtype bool, wlrctl bool)
	Key(ctx context.Context, name string, repeat int) error
	Combo(ctx context.Context, mod, key string) error
	Text(ctx context.Context, text string) error
	Pointer(ctx context.Context, dx, dy int) error
	Click(ctx context.Context, button string) error
	Scroll(ctx context.Context, dy, dx int) error
}

// AudioState is the selected sink and its current volume.
type AudioState struct {
	Sink   string
	Volume float64
	Muted  bool
}

// Audio controls the PipeWire-Pulse sink used by the TV.
type Audio interface {
	State(ctx context.Context) (AudioState, error)
	SetVolume(ctx context.Context, value float64) error
	SetMuted(ctx context.Context, muted bool) error
}

// Health is the JSON returned by GET /api/health. Pointer fields become null
// when the value is unknown, matching the old Python response.
type Health struct {
	OK        bool     `json:"ok"`
	Version   string   `json:"version,omitempty"`
	Wayland   *string  `json:"wayland"`
	Wtype     bool     `json:"wtype"`
	Wlrctl    bool     `json:"wlrctl"`
	Sink      *string  `json:"sink"`
	Volume    *float64 `json:"volume"`
	Muted     *bool    `json:"muted"`
	LastError *string  `json:"last_error"`
}

// Service validates actions and applies them to the kiosk.
type Service struct {
	input  Input
	audio  Audio
	settle time.Duration

	keyLock sync.Mutex

	errMu     sync.RWMutex
	lastError string
}

// NewService builds a service. settle is the delay after Alt+Home.
func NewService(input Input, audio Audio, settle time.Duration) *Service {
	if settle < 500*time.Millisecond {
		settle = 500 * time.Millisecond
	}
	return &Service{input: input, audio: audio, settle: settle}
}

// Dispatch runs one action from the phone page.
func (s *Service) Dispatch(ctx context.Context, value any) error {
	payload, ok := value.(map[string]any)
	if !ok || payload == nil {
		return badRequest("expected a JSON object")
	}

	kind, _ := payload["type"].(string)
	switch kind {
	case "key":
		name, _ := payload["key"].(string)
		if _, ok := allowedKeys[name]; !ok {
			return badRequest("unsupported key")
		}
		repeat, err := intOr(payload["repeat"], 1, 1, 10)
		if err != nil {
			return err
		}
		s.keyLock.Lock()
		defer s.keyLock.Unlock()
		return s.input.Key(ctx, name, repeat)

	case "combo":
		mod, _ := payload["mod"].(string)
		name, _ := payload["key"].(string)
		if _, ok := allowedModifiers[mod]; !ok {
			return badRequest("unsupported modifier")
		}
		if _, ok := allowedKeys[name]; !ok {
			return badRequest("unsupported key")
		}
		s.keyLock.Lock()
		defer s.keyLock.Unlock()
		return s.input.Combo(ctx, mod, name)

	case "text":
		text := normalizeText(textValue(payload["text"]))
		if text == "" {
			return badRequest("empty text")
		}
		s.keyLock.Lock()
		defer s.keyLock.Unlock()
		return s.input.Text(ctx, text)

	case "pointer":
		dx, err := intOr(payload["dx"], 0, -500, 500)
		if err != nil {
			return err
		}
		dy, err := intOr(payload["dy"], 0, -500, 500)
		if err != nil {
			return err
		}
		return s.input.Pointer(ctx, dx, dy)

	case "click":
		button := "left"
		if raw, ok := payload["button"]; ok {
			value, ok := raw.(string)
			if !ok {
				return badRequest("unsupported button")
			}
			button = value
		}
		if _, ok := allowedButtons[button]; !ok {
			return badRequest("unsupported button")
		}
		return s.input.Click(ctx, button)

	case "scroll":
		dy, err := intOr(payload["dy"], 0, -50, 50)
		if err != nil {
			return err
		}
		dx, err := intOr(payload["dx"], 0, -50, 50)
		if err != nil {
			return err
		}
		return s.input.Scroll(ctx, dy, dx)

	case "launch":
		index, err := clampInt(payload["index"], 0, 99)
		if err != nil {
			return err
		}
		columns, err := clampInt(payload["columns"], 1, 8)
		if err != nil {
			return err
		}
		return s.launch(ctx, index, columns)

	case "volume":
		value, err := floatValue(payload["value"])
		if err != nil {
			return badRequest("invalid volume")
		}
		if value < 0 || value > 1 {
			return badRequest("volume out of range")
		}
		return s.audio.SetVolume(ctx, value)

	case "mute":
		if toggle, _ := payload["toggle"].(bool); toggle {
			state, err := s.audio.State(ctx)
			if err != nil {
				return err
			}
			return s.audio.SetMuted(ctx, !state.Muted)
		}
		raw, ok := payload["muted"]
		if !ok {
			return badRequest("missing mute value")
		}
		muted, ok := raw.(bool)
		if !ok {
			return badRequest("missing mute value")
		}
		return s.audio.SetMuted(ctx, muted)

	default:
		return badRequest("unknown action")
	}
}

func (s *Service) launch(ctx context.Context, index, columns int) error {
	if !s.keyLock.TryLock() {
		return ErrBusy
	}
	defer s.keyLock.Unlock()

	if err := s.input.Combo(ctx, "alt", "Home"); err != nil {
		return err
	}
	if err := sleepContext(ctx, s.settle); err != nil {
		return err
	}

	row, column := index/columns, index%columns
	for range row {
		if err := s.input.Key(ctx, "Down", 1); err != nil {
			return err
		}
	}
	for range column {
		if err := s.input.Key(ctx, "Right", 1); err != nil {
			return err
		}
	}
	return s.input.Key(ctx, "Return", 1)
}

// Health reports the kiosk and audio state. It always returns 200 from the
// HTTP layer; ok=false means the TV is offline.
func (s *Service) Health(ctx context.Context) Health {
	display, hasDisplay := s.input.Display()
	wtype, wlrctl := s.input.HasTools()

	health := Health{
		OK:      hasDisplay && wtype && wlrctl,
		Wtype:   wtype,
		Wlrctl:  wlrctl,
		Wayland: optionalString(display, hasDisplay),
	}

	state, err := s.audio.State(ctx)
	if err != nil {
		// Match the Python API: audio failures are reported but do not make
		// the whole kiosk offline.
		health.LastError = optionalString(err.Error(), true)
	} else {
		health.Sink = optionalString(state.Sink, state.Sink != "")
		health.Volume = optionalFloat(state.Volume, true)
		health.Muted = optionalBool(state.Muted, true)
	}

	if health.LastError == nil {
		health.LastError = s.LastError()
	}
	return health
}

// RecordError stores and logs a helper failure for /api/health.
func (s *Service) RecordError(err error) {
	if err == nil {
		s.ClearError()
		return
	}
	s.errMu.Lock()
	s.lastError = err.Error()
	s.errMu.Unlock()
	log.Printf("travel-tv: %v", err)
}

// ClearError clears the last helper failure.
func (s *Service) ClearError() {
	s.errMu.Lock()
	s.lastError = ""
	s.errMu.Unlock()
}

// LastError returns the last helper failure, or nil when everything is fine.
func (s *Service) LastError() *string {
	s.errMu.RLock()
	defer s.errMu.RUnlock()
	if s.lastError == "" {
		return nil
	}
	value := s.lastError
	return &value
}

func optionalString(value string, ok bool) *string {
	if !ok {
		return nil
	}
	return &value
}

func optionalFloat(value float64, ok bool) *float64 {
	if !ok {
		return nil
	}
	return &value
}

func optionalBool(value bool, ok bool) *bool {
	if !ok {
		return nil
	}
	return &value
}

func clampInt(value any, low, high int) (int, error) {
	if value == nil {
		return 0, badRequest("expected a number")
	}
	var number int
	switch v := value.(type) {
	case int:
		number = v
	case int64:
		number = int(v)
	case float64:
		number = int(v)
	case json.Number:
		parsed, err := v.Int64()
		if err != nil {
			return 0, badRequest("expected a number")
		}
		number = int(parsed)
	case string:
		parsed, err := strconv.Atoi(v)
		if err != nil {
			return 0, badRequest("expected a number")
		}
		number = parsed
	default:
		return 0, badRequest("expected a number")
	}
	if number < low {
		number = low
	}
	if number > high {
		number = high
	}
	return number, nil
}

func intOr(value any, fallback, low, high int) (int, error) {
	if value == nil {
		return fallback, nil
	}
	return clampInt(value, low, high)
}

func floatValue(value any) (float64, error) {
	switch v := value.(type) {
	case float64:
		return v, nil
	case int:
		return float64(v), nil
	case json.Number:
		return v.Float64()
	case string:
		return strconv.ParseFloat(v, 64)
	default:
		return 0, badRequest("invalid volume")
	}
}

func textValue(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	default:
		return fmt.Sprint(v)
	}
}

func normalizeText(value string) string {
	text := strings.Join(strings.Fields(value), " ")
	runes := []rune(text)
	if len(runes) > 300 {
		return string(runes[:300])
	}
	return text
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
