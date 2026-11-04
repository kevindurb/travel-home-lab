package tv

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeInput struct {
	display string
	wtype   bool
	wlrctl  bool

	mu    sync.Mutex
	calls []string
	err   error
	block chan struct{}
}

func (f *fakeInput) Display() (string, bool) { return f.display, f.display != "" }
func (f *fakeInput) HasTools() (bool, bool)  { return f.wtype, f.wlrctl }

func (f *fakeInput) record(call string) error {
	f.mu.Lock()
	f.calls = append(f.calls, call)
	f.mu.Unlock()
	if f.block != nil {
		<-f.block
	}
	return f.err
}

func (f *fakeInput) Key(ctx context.Context, name string, repeat int) error {
	return f.record("key:" + name + ":" + strconv.Itoa(repeat))
}
func (f *fakeInput) Combo(ctx context.Context, mod, key string) error {
	return f.record("combo:" + mod + "+" + key)
}
func (f *fakeInput) Text(ctx context.Context, text string) error {
	return f.record("text:" + text)
}
func (f *fakeInput) Pointer(ctx context.Context, dx, dy int) error {
	return f.record("pointer")
}
func (f *fakeInput) Click(ctx context.Context, button string) error {
	return f.record("click:" + button)
}
func (f *fakeInput) Scroll(ctx context.Context, dy, dx int) error {
	return f.record("scroll")
}

func (f *fakeInput) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

type fakeAudio struct {
	state    AudioState
	stateErr error
	setErr   error
	volume   *float64
	muted    *bool
}

func (f *fakeAudio) State(ctx context.Context) (AudioState, error) {
	return f.state, f.stateErr
}
func (f *fakeAudio) SetVolume(ctx context.Context, value float64) error {
	f.volume = &value
	return f.setErr
}
func (f *fakeAudio) SetMuted(ctx context.Context, muted bool) error {
	f.muted = &muted
	return f.setErr
}

func newTestService(input Input, audio Audio) *Service {
	service := NewService(input, audio, time.Second)
	service.settle = 0
	return service
}

func TestNormalizeText(t *testing.T) {
	if got := normalizeText("  hello\n\tworld  "); got != "hello world" {
		t.Fatalf("normalizeText = %q", got)
	}
	long := strings.Repeat("é", 400)
	if got := normalizeText(long); len([]rune(got)) != 300 {
		t.Fatalf("normalizeText length = %d", len([]rune(got)))
	}
}

func TestClampInt(t *testing.T) {
	cases := []struct {
		value any
		want  int
	}{
		{float64(2.9), 2},
		{json.Number("3"), 3},
		{"4", 4},
		{int(5), 5},
	}
	for _, test := range cases {
		got, err := clampInt(test.value, 0, 10)
		if err != nil || got != test.want {
			t.Fatalf("clampInt(%v) = %d, %v", test.value, got, err)
		}
	}
	if _, err := clampInt(nil, 0, 10); err == nil {
		t.Fatal("clampInt(nil) should fail")
	}
}

func TestDispatchValidatesAllowlists(t *testing.T) {
	service := newTestService(&fakeInput{}, &fakeAudio{})
	cases := []string{
		`{"type":"key","key":"F1"}`,
		`{"type":"combo","mod":"hyper","key":"Home"}`,
		`{"type":"click","button":"back"}`,
		`{"type":"warp"}`,
		`[]`,
	}
	for _, body := range cases {
		var payload any
		if err := json.Unmarshal([]byte(body), &payload); err != nil {
			t.Fatalf("decode %s: %v", body, err)
		}
		if err := service.Dispatch(context.Background(), payload); err == nil {
			t.Fatalf("Dispatch(%s) should fail", body)
		}
	}
}

func TestDispatchKeyAndText(t *testing.T) {
	input := &fakeInput{}
	service := newTestService(input, &fakeAudio{})

	if err := service.Dispatch(context.Background(), map[string]any{
		"type": "key", "key": "Right", "repeat": float64(2),
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.Dispatch(context.Background(), map[string]any{
		"type": "text", "text": "  hello\nworld ",
	}); err != nil {
		t.Fatal(err)
	}
	want := []string{"key:Right:2", "text:hello world"}
	got := input.Calls()
	if len(got) != len(want) {
		t.Fatalf("calls = %v", got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("calls = %v", got)
		}
	}
}

func TestLaunchSequence(t *testing.T) {
	input := &fakeInput{}
	service := newTestService(input, &fakeAudio{})

	if err := service.launch(context.Background(), 5, 4); err != nil {
		t.Fatal(err)
	}
	want := []string{"combo:alt+Home", "key:Down:1", "key:Right:1", "key:Return:1"}
	got := input.Calls()
	if len(got) != len(want) {
		t.Fatalf("calls = %v", got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("calls = %v", got)
		}
	}
}

func TestLaunchBusy(t *testing.T) {
	service := newTestService(&fakeInput{}, &fakeAudio{})
	service.keyLock.Lock()
	defer service.keyLock.Unlock()

	err := service.Dispatch(context.Background(), map[string]any{
		"type": "launch", "index": float64(1), "columns": float64(4),
	})
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("err = %v, want ErrBusy", err)
	}
}

func TestVolumeAndMute(t *testing.T) {
	audio := &fakeAudio{state: AudioState{Sink: "hdmi", Muted: false}}
	service := newTestService(&fakeInput{}, audio)

	if err := service.Dispatch(context.Background(), map[string]any{
		"type": "volume", "value": float64(0.5),
	}); err != nil {
		t.Fatal(err)
	}
	if audio.volume == nil || *audio.volume != 0.5 {
		t.Fatalf("volume = %v", audio.volume)
	}

	if err := service.Dispatch(context.Background(), map[string]any{
		"type": "mute", "toggle": true,
	}); err != nil {
		t.Fatal(err)
	}
	if audio.muted == nil || *audio.muted != true {
		t.Fatalf("muted = %v", audio.muted)
	}

	if err := service.Dispatch(context.Background(), map[string]any{
		"type": "volume", "value": float64(1.5),
	}); err == nil {
		t.Fatal("out-of-range volume should fail")
	}
}

func TestHealth(t *testing.T) {
	input := &fakeInput{display: "wayland-0", wtype: true, wlrctl: true}
	audio := &fakeAudio{state: AudioState{Sink: "hdmi-stereo", Volume: 0.4, Muted: false}}
	service := newTestService(input, audio)

	health := service.Health(context.Background())
	if !health.OK || health.Wayland == nil || *health.Wayland != "wayland-0" {
		t.Fatalf("health = %+v", health)
	}
	if health.Volume == nil || *health.Volume != 0.4 {
		t.Fatalf("health = %+v", health)
	}

	audio.stateErr = errors.New("no pulse")
	health = service.Health(context.Background())
	if !health.OK || health.LastError == nil || *health.LastError != "no pulse" {
		t.Fatalf("health = %+v", health)
	}

	input.display = ""
	health = service.Health(context.Background())
	if health.OK || health.Wayland != nil {
		t.Fatalf("health = %+v", health)
	}
}
