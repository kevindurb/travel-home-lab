package tv

import (
	"testing"

	"github.com/jfreymuth/pulse/proto"
)

func TestChooseSinkPrefersHDMI(t *testing.T) {
	sinks := proto.GetSinkInfoListReply{
		{SinkIndex: 1, SinkName: "alsa_output.default"},
		{SinkIndex: 2, SinkName: "alsa_output.platform-hdmi-stereo"},
	}
	sink := chooseSink(sinks, "alsa_output.default")
	if sink == nil || sink.SinkIndex != 2 {
		t.Fatalf("sink = %+v", sink)
	}
}

func TestChooseSinkUsesDefault(t *testing.T) {
	sinks := proto.GetSinkInfoListReply{
		{SinkIndex: 1, SinkName: "first"},
		{SinkIndex: 2, SinkName: "default"},
	}
	sink := chooseSink(sinks, "default")
	if sink == nil || sink.SinkIndex != 2 {
		t.Fatalf("sink = %+v", sink)
	}
}

func TestChooseSinkFallsBackToFirst(t *testing.T) {
	sinks := proto.GetSinkInfoListReply{
		{SinkIndex: 1, SinkName: "first"},
		{SinkIndex: 2, SinkName: "second"},
	}
	sink := chooseSink(sinks, "missing")
	if sink == nil || sink.SinkIndex != 1 {
		t.Fatalf("sink = %+v", sink)
	}
	if chooseSink(nil, "") != nil {
		t.Fatal("empty list should return nil")
	}
}

func TestChooseSinkUsesDescription(t *testing.T) {
	sinks := proto.GetSinkInfoListReply{
		{
			SinkIndex:  1,
			SinkName:   "alsa_output.platform-1",
			Properties: proto.PropList{"device.description": proto.PropListString("HDMI Audio")},
		},
	}
	sink := chooseSink(sinks, "missing")
	if sink == nil || sink.SinkIndex != 1 {
		t.Fatalf("sink = %+v", sink)
	}
}
