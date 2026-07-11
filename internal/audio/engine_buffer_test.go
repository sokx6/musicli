package audio

import "testing"

func TestPlayerBufferTargetsOneHundredMilliseconds(t *testing.T) {
	want := SampleRate * ChannelCount * BitDepthInBytes / 10
	if playerBufferSizeBytes != want {
		t.Fatalf("player buffer = %d bytes, want %d", playerBufferSizeBytes, want)
	}
}

func TestSpectrumPCMChunksAreNoLongerThanOneFFTWindow(t *testing.T) {
	fftWindowBytes := SpectrumFFTSize * ChannelCount * BitDepthInBytes
	if spectrumPCMChunkSize > fftWindowBytes {
		t.Fatalf("spectrum chunk = %d bytes, want <= one FFT window (%d)", spectrumPCMChunkSize, fftWindowBytes)
	}
}
