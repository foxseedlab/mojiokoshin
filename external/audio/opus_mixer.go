package audio

import (
	"encoding/binary"
	"sync"

	"github.com/foxseedlab/mojiokoshin/internal/audio"
	"github.com/hraban/opus"
)

const (
	sampleRate      = 48000
	channels        = 2
	frameSizeMs     = 20
	samplesPerFrame = sampleRate * frameSizeMs * channels / 1000
)

type OpusMixer struct {
	mu       sync.Mutex
	decoders map[string]*opus.Decoder
	queues   map[string]*frameQueue
	closed   bool
}

type frameQueue struct {
	frames [][]int16
}

func (q *frameQueue) push(frame []int16) {
	q.frames = append(q.frames, frame)
}

func (q *frameQueue) pop() ([]int16, bool) {
	if len(q.frames) == 0 {
		return nil, false
	}
	f := q.frames[0]
	q.frames = q.frames[1:]
	return f, true
}

func (q *frameQueue) hasFrame() bool {
	return len(q.frames) > 0
}

func NewOpusMixer() audio.Mixer {
	return &OpusMixer{
		decoders: make(map[string]*opus.Decoder),
		queues:   make(map[string]*frameQueue),
	}
}

func (m *OpusMixer) WriteOpusPacket(userID string, opusData []byte) {
	if len(opusData) == 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	dec, ok := m.decoders[userID]
	if !ok {
		var err error
		dec, err = opus.NewDecoder(sampleRate, channels)
		if err != nil {
			return
		}
		m.decoders[userID] = dec
		m.queues[userID] = &frameQueue{}
	}
	q := m.queues[userID]
	pcm := make([]int16, samplesPerFrame)
	n, err := dec.Decode(opusData, pcm)
	if err != nil {
		return
	}
	if n > 0 {
		totalSamples := n * channels
		if totalSamples > samplesPerFrame {
			totalSamples = samplesPerFrame
		}
		frame := make([]int16, totalSamples)
		copy(frame, pcm[:totalSamples])
		q.push(frame)
	}
}

func (m *OpusMixer) ReadMixedPCM(buf []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return 0, nil
	}
	if !hasQueuedFrames(m.queues) {
		return 0, nil
	}
	mixed := make([]int16, samplesPerFrame)
	m.mixQueuedFrames(mixed)
	return writeMixedPCM(buf, mixed), nil
}

func hasQueuedFrames(queues map[string]*frameQueue) bool {
	for _, q := range queues {
		if q.hasFrame() {
			return true
		}
	}
	return false
}

func (m *OpusMixer) mixQueuedFrames(mixed []int16) {
	for _, q := range m.queues {
		frame, ok := q.pop()
		if !ok {
			continue
		}
		for i := 0; i < len(frame) && i < samplesPerFrame; i++ {
			mixed[i] = clampPCM(int32(mixed[i]) + int32(frame[i]))
		}
	}
}

func clampPCM(v int32) int16 {
	if v > 32767 {
		return 32767
	}
	if v < -32768 {
		return -32768
	}
	return int16(v)
}

func writeMixedPCM(buf []byte, mixed []int16) int {
	toWrite := len(buf) / 2
	if toWrite > samplesPerFrame {
		toWrite = samplesPerFrame
	}
	for i := 0; i < toWrite; i++ {
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(mixed[i]))
	}
	return toWrite * 2
}

func (m *OpusMixer) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	m.decoders = nil
	m.queues = nil
}
