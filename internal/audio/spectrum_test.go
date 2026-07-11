package audio

import (
	"encoding/binary"
	"math"
	"testing"
)

func TestSpectrumAnalyzerOrdersSineWaveBandsByFrequency(t *testing.T) {
	low, err := NewSpectrumAnalyzer()
	if err != nil {
		t.Fatal(err)
	}
	high, err := NewSpectrumAnalyzer()
	if err != nil {
		t.Fatal(err)
	}
	low.WritePCM(sinePCM(440, SpectrumFFTSize, SampleRate))
	high.WritePCM(sinePCM(4000, SpectrumFFTSize, SampleRate))

	lowPeak, lowLevel := peakLevel(low.Levels(24))
	highPeak, highLevel := peakLevel(high.Levels(24))
	if lowPeak >= highPeak {
		t.Fatalf("440Hz peak band = %d, 4kHz peak band = %d; want low < high", lowPeak, highPeak)
	}
	if lowLevel < 0.2 || highLevel < 0.2 {
		t.Fatalf("peak levels = %.3f, %.3f; want both >= 0.2", lowLevel, highLevel)
	}
}

func peakLevel(levels []float64) (int, float64) {
	peak := 0
	for i := range levels {
		if levels[i] > levels[peak] {
			peak = i
		}
	}
	return peak, levels[peak]
}

func TestSpectrumAnalyzerReturnsSilenceWithoutPCM(t *testing.T) {
	analyzer, err := NewSpectrumAnalyzer()
	if err != nil {
		t.Fatal(err)
	}
	for i, level := range analyzer.Levels(8) {
		if level != 0 {
			t.Fatalf("level %d = %f, want 0", i, level)
		}
	}
}

func TestSpectrumAnalyzerResetClearsPriorPCM(t *testing.T) {
	analyzer, err := NewSpectrumAnalyzer()
	if err != nil {
		t.Fatal(err)
	}
	analyzer.WritePCM(sinePCM(440, SpectrumFFTSize, SampleRate))
	analyzer.Reset()
	for i, level := range analyzer.Levels(8) {
		if level != 0 {
			t.Fatalf("level %d after reset = %f, want 0", i, level)
		}
	}
}

func TestEngineRejectsStaleSpectrumPCM(t *testing.T) {
	analyzer, err := NewSpectrumAnalyzer()
	if err != nil {
		t.Fatal(err)
	}
	engine := &Engine{spectrum: analyzer, spectrumEpoch: 2}
	engine.writeSpectrumPCM(1, sinePCM(440, SpectrumFFTSize, SampleRate))
	for i, level := range analyzer.Levels(8) {
		if level != 0 {
			t.Fatalf("stale level %d = %f, want 0", i, level)
		}
	}
}

func sinePCM(freq float64, frames, sampleRate int) []byte {
	p := make([]byte, frames*ChannelCount*BitDepthInBytes)
	for i := 0; i < frames; i++ {
		sample := int16(math.Sin(2*math.Pi*freq*float64(i)/float64(sampleRate)) * 28000)
		for channel := 0; channel < ChannelCount; channel++ {
			offset := (i*ChannelCount + channel) * BitDepthInBytes
			binary.LittleEndian.PutUint16(p[offset:], uint16(sample))
		}
	}
	return p
}
