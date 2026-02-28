package audio

type Mixer interface {
	WriteOpusPacket(userID string, opus []byte)
	ReadMixedPCM(buf []byte) (int, error)
	Close()
}

type MixerFactory func() Mixer
