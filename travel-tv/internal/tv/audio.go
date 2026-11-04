package tv

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jfreymuth/pulse"
	"github.com/jfreymuth/pulse/proto"
)

// PulseAudio controls the TV's HDMI sink through the PipeWire-Pulse socket.
type PulseAudio struct {
	timeout time.Duration
}

// NewPulseAudio builds a client for the host's pipewire-pulse service.
func NewPulseAudio() *PulseAudio {
	return &PulseAudio{timeout: 2 * time.Second}
}

func (p *PulseAudio) State(ctx context.Context) (AudioState, error) {
	client, err := p.client()
	if err != nil {
		return AudioState{}, err
	}
	defer client.Close()

	sink, _, err := p.resolve(client)
	if err != nil {
		return AudioState{}, err
	}

	volume := 0.0
	if len(sink.ChannelVolumes) > 0 {
		volume = sink.ChannelVolumes.Avg().Linear()
	}

	name := sink.SinkName
	if name == "" {
		name = sink.Device
	}
	if name == "" {
		name = "default"
	}

	return AudioState{Sink: name, Volume: volume, Muted: sink.Mute}, nil
}

func (p *PulseAudio) SetVolume(ctx context.Context, value float64) error {
	client, err := p.client()
	if err != nil {
		return err
	}
	defer client.Close()

	sink, _, err := p.resolve(client)
	if err != nil {
		return err
	}

	channels := len(sink.ChannelMap)
	if channels == 0 {
		channels = len(sink.ChannelVolumes)
	}
	if channels == 0 {
		channels = 1
	}
	volumes := make(proto.ChannelVolumes, channels)
	for index := range volumes {
		volumes[index] = proto.LinearVolume(value)
	}

	return client.RawRequest(&proto.SetSinkVolume{
		SinkIndex:      sink.SinkIndex,
		ChannelVolumes: volumes,
	}, nil)
}

func (p *PulseAudio) SetMuted(ctx context.Context, muted bool) error {
	client, err := p.client()
	if err != nil {
		return err
	}
	defer client.Close()

	sink, _, err := p.resolve(client)
	if err != nil {
		return err
	}

	return client.RawRequest(&proto.SetSinkMute{
		SinkIndex: sink.SinkIndex,
		Mute:      muted,
	}, nil)
}

func (p *PulseAudio) client() (*pulse.Client, error) {
	return pulse.NewClient(
		pulse.ClientTimeout(p.timeout),
		pulse.ClientApplicationName("Travel TV"),
	)
}

func (p *PulseAudio) resolve(client *pulse.Client) (*proto.GetSinkInfoReply, *proto.GetServerInfoReply, error) {
	var server proto.GetServerInfoReply
	if err := client.RawRequest(&proto.GetServerInfo{}, &server); err != nil {
		return nil, nil, err
	}

	var sinks proto.GetSinkInfoListReply
	if err := client.RawRequest(&proto.GetSinkInfoList{}, &sinks); err != nil {
		return nil, nil, err
	}

	sink := chooseSink(sinks, server.DefaultSinkName)
	if sink == nil {
		return nil, nil, errors.New("no audio sink")
	}
	return sink, &server, nil
}

// chooseSink prefers an HDMI sink, then PulseAudio's default sink, then the
// first available sink. It mirrors the old wpctl behaviour.
func chooseSink(sinks proto.GetSinkInfoListReply, defaultName string) *proto.GetSinkInfoReply {
	if len(sinks) == 0 {
		return nil
	}
	for _, sink := range sinks {
		if sink != nil && strings.Contains(strings.ToLower(sinkText(sink)), "hdmi") {
			return sink
		}
	}
	for _, sink := range sinks {
		if sink != nil && sink.SinkName == defaultName {
			return sink
		}
	}
	return sinks[0]
}

func sinkText(sink *proto.GetSinkInfoReply) string {
	parts := []string{sink.SinkName, sink.Device}
	for _, key := range []string{"device.description", "node.description", "device.name"} {
		if value, ok := sink.Properties[key]; ok {
			parts = append(parts, value.String())
		}
	}
	return strings.Join(parts, " ")
}
