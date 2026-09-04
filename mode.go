package av

// Mode controls which streams are decoded.
type Mode int

const (
	// ModeAuto automatically detects and decodes available video and audio streams.
	ModeAuto Mode = iota
	// ModeVideoAndAudio decodes both video and audio channels.
	ModeVideoAndAudio
	// ModeVideoOnly decodes video frames; skips audio.
	ModeVideoOnly
	// ModeAudioOnly processes only audio streams; skips video rendering.
	ModeAudioOnly
)

func (m Mode) cval() int {
	switch m {
	case ModeVideoAndAudio:
		return 1
	case ModeVideoOnly:
		return 2
	case ModeAudioOnly:
		return 3
	default:
		return 0
	}
}