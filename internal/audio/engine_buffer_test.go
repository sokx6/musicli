package audio

import "testing"

func TestPlayerBufferTargetsOneHundredMilliseconds(t *testing.T) {
	want := SampleRate * ChannelCount * BitDepthInBytes / 10
	if playerBufferSizeBytes != want {
		t.Fatalf("player buffer = %d bytes, want %d", playerBufferSizeBytes, want)
	}
}

func TestSpectrumPCMChunkSizeFollowsConfiguredRefreshRate(t *testing.T) {
	if got, want := spectrumPCMChunkSizeForHz(DefaultSpectrumUpdateHz), 3840; got != want {
		t.Fatalf("default spectrum chunk = %d bytes, want %d", got, want)
	}
	if got, want := spectrumPCMChunkSizeForHz(100), 1920; got != want {
		t.Fatalf("100Hz spectrum chunk = %d bytes, want %d", got, want)
	}
}
