//go:build !opus

package audio

import "github.com/foxseedlab/mojiokoshin/internal/audio"

type noopMixer struct{}

func NewOpusMixer() audio.Mixer {
	return &noopMixer{}
}

func (m *noopMixer) WriteOpusPacket(_ string, _ []byte) {}

func (m *noopMixer) ReadMixedPCM(_ []byte) (int, error) {
	return 0, nil
}

func (m *noopMixer) Close() {}
