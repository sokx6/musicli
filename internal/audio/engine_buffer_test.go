package audio

import "testing"

func TestPlayerBufferTargetsOneHundredMilliseconds(t *testing.T) {
	want := SampleRate * ChannelCount * BitDepthInBytes / 10
	if playerBufferSizeBytes != want {
		t.Fatalf("player buffer = %d bytes, want %d", playerBufferSizeBytes, want)
	}
}
