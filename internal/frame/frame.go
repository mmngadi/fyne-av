package frame

// Video is a decoded video frame passed from the controller to the widget.
type Video struct {
	RGBA  []byte
	Width int
	Height int
	PtsMs int64
}

// Audio is a decoded audio frame passed to the audio pipeline.
type Audio struct {
	PCM        []int16
	NSamples   int
	Channels   int
	SampleRate int
	PtsMs      int64
}