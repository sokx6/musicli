package audio

import (
	"encoding/binary"
	"math"
	"sync"

	algofft "github.com/cwbudde/algo-fft"
)

const SpectrumFFTSize = 2048

// SpectrumAnalyzer retains recent PCM frames and turns them into normalized
// display bands. It deliberately owns no goroutine: decoding continues even
// when the UI does not ask for a spectrum.
type SpectrumAnalyzer struct {
	mu      sync.RWMutex
	samples []float32
	writeAt int
	filled  int
	plan    *algofft.PlanReal
}

func NewSpectrumAnalyzer() (*SpectrumAnalyzer, error) {
	plan, err := algofft.NewPlanReal(SpectrumFFTSize)
	if err != nil {
		return nil, err
	}
	return &SpectrumAnalyzer{
		samples: make([]float32, SpectrumFFTSize),
		plan:    plan,
	}, nil
}

// WritePCM accepts interleaved signed 16-bit stereo samples. Invalid trailing
// bytes are ignored because ffmpeg pipe reads need not end on a frame boundary.
func (s *SpectrumAnalyzer) WritePCM(pcm []byte) {
	frameBytes := ChannelCount * BitDepthInBytes
	s.mu.Lock()
	defer s.mu.Unlock()
	for offset := 0; offset+frameBytes <= len(pcm); offset += frameBytes {
		left := int16(binary.LittleEndian.Uint16(pcm[offset:]))
		right := int16(binary.LittleEndian.Uint16(pcm[offset+BitDepthInBytes:]))
		s.samples[s.writeAt] = float32(left+right) / (2 * 32768)
		s.writeAt = (s.writeAt + 1) % len(s.samples)
		if s.filled < len(s.samples) {
			s.filled++
		}
	}
}

// Reset discards the prior track's PCM so its bars cannot leak into the next
// track, pause, or stopped state.
func (s *SpectrumAnalyzer) Reset() {
	s.mu.Lock()
	clear(s.samples)
	s.writeAt = 0
	s.filled = 0
	s.mu.Unlock()
}

// Levels returns logarithmically distributed bands in the inclusive 0..1
// range. A short or silent buffer deliberately returns zero levels.
func (s *SpectrumAnalyzer) Levels(bands int) []float64 {
	levels := make([]float64, max(0, bands))
	if bands <= 0 {
		return levels
	}

	input := make([]float32, SpectrumFFTSize)
	s.mu.RLock()
	if s.filled < SpectrumFFTSize {
		s.mu.RUnlock()
		return levels
	}
	for i := range input {
		input[i] = s.samples[(s.writeAt+i)%len(s.samples)]
	}
	s.mu.RUnlock()

	// Hann window limits spectral leakage from arbitrary PCM frame boundaries.
	for i := range input {
		input[i] *= float32(0.5 * (1 - math.Cos(2*math.Pi*float64(i)/float64(len(input)-1))))
	}
	output := make([]complex64, SpectrumFFTSize/2+1)
	if err := s.plan.Forward(output, input); err != nil {
		return levels
	}

	minBin := 1
	maxBin := len(output) - 1
	for band := range levels {
		start := logarithmicBin(band, bands, minBin, maxBin)
		end := logarithmicBin(band+1, bands, minBin, maxBin)
		if end <= start {
			end = start + 1
		}
		peak := 0.0
		for bin := start; bin < end && bin < len(output); bin++ {
			v := output[bin]
			magnitude := math.Hypot(float64(real(v)), float64(imag(v))) / float64(SpectrumFFTSize)
			if magnitude > peak {
				peak = magnitude
			}
		}
		// A square-root response keeps quieter musical detail visible.
		levels[band] = min(1, math.Sqrt(peak*8))
	}
	return levels
}

func logarithmicBin(index, total, minBin, maxBin int) int {
	if index <= 0 {
		return minBin
	}
	if index >= total {
		return maxBin
	}
	ratio := float64(index) / float64(total)
	return int(math.Round(float64(minBin) * math.Pow(float64(maxBin)/float64(minBin), ratio)))
}
