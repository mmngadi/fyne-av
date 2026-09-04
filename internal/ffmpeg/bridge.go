package ffmpeg

// #include <stdlib.h>
// #include <string.h>
// #include "ffmpeg.h"
import "C"

import (
	"errors"
	"io"
	"unsafe"
)

// Mode constants mirrored from the C side (ffmpeg.c).
const (
	modeAuto = 0
	modeVA   = 1
	modeV    = 2
	modeA    = 3
)

// Frame type constants.
const (
	FrameNone  = 0
	FrameVideo = 1
	FrameAudio = 2
	FrameEOF   = 3
)

// Player is the Go-facing wrapper around the C FFmpeg decoder.
type Player struct {
	c *C.AVPlayer
}

// Open opens a media file at url with the given mode (0=auto,1=va,2=v,3,a).
func Open(url string, mode int) (*Player, error) {
	curl := C.CString(url)
	defer C.free(unsafe.Pointer(curl))
	var cerr *C.char
	p := C.av_player_open(curl, C.int(mode), &cerr)
	if p == nil {
		msg := "open failed"
		if cerr != nil {
			msg = C.GoString(cerr)
			C.free(unsafe.Pointer(cerr))
		}
		return nil, errors.New(msg)
	}
	return &Player{c: p}, nil
}

// Close releases all resources.
func (p *Player) Close() {
	if p == nil || p.c == nil {
		return
	}
	C.av_player_close(p.c)
	p.c = nil
}

// HasVideo returns whether video decoding is active.
func (p *Player) HasVideo() bool {
	return C.av_player_has_video(p.c) != 0
}

// HasAudio returns whether audio decoding is active.
func (p *Player) HasAudio() bool {
	return C.av_player_has_audio(p.c) != 0
}

// DurationMs returns the media duration in milliseconds.
func (p *Player) DurationMs() int64 {
	return int64(C.av_player_duration_ms(p.c))
}

// VideoFrame holds a decoded RGBA video frame.
type VideoFrame struct {
	RGBA   []byte
	Width  int
	Height int
	PtsMs  int64
}

// AudioFrame holds a decoded PCM S16 audio frame.
type AudioFrame struct {
	PCM        []int16
	NSamples   int
	Channels   int
	SampleRate int
	PtsMs      int64
}

// FrameResult is the result of a single decode step.
type FrameResult struct {
	Type  int
	Video VideoFrame
	Audio AudioFrame
}

// NextFrame decodes the next frame (video or audio). Returns io.EOF on end.
func (p *Player) NextFrame() (FrameResult, error) {
	var cr C.AVFrameResult
	ret := C.av_player_next_frame(p.c, &cr)
	if ret == C.AV_FRAME_EOF || cr.kind == C.AV_FRAME_EOF {
		return FrameResult{Type: FrameEOF}, io.EOF
	}
	if ret < 0 || cr.kind == C.AV_FRAME_NONE {
		return FrameResult{Type: FrameNone}, errors.New("decode error")
	}

	result := FrameResult{Type: int(cr.kind)}

	if cr.kind == C.AV_FRAME_VIDEO {
		w := int(cr.video.width)
		h := int(cr.video.height)
		size := w * h * 4
		rgba := make([]byte, size)
		if cr.video.data != nil && size > 0 {
			C.memcpy(unsafe.Pointer(&rgba[0]), unsafe.Pointer(cr.video.data), C.size_t(size))
		}
		result.Video = VideoFrame{
			RGBA:   rgba,
			Width:  w,
			Height: h,
			PtsMs:  int64(cr.video.pts_ms),
		}
	}

	if cr.kind == C.AV_FRAME_AUDIO {
		n := int(cr.audio.n_samples) * int(cr.audio.channels)
		pcm := make([]int16, n)
		if cr.audio.data != nil && n > 0 {
			C.memcpy(unsafe.Pointer(&pcm[0]), unsafe.Pointer(cr.audio.data), C.size_t(n*2))
		}
		result.Audio = AudioFrame{
			PCM:        pcm,
			NSamples:   int(cr.audio.n_samples),
			Channels:   int(cr.audio.channels),
			SampleRate: int(cr.audio.sample_rate),
			PtsMs:      int64(cr.audio.pts_ms),
		}
	}

	return result, nil
}

// SeekTo seeks to target_ms.
func (p *Player) SeekTo(targetMs int64) error {
	ret := C.av_player_seek(p.c, C.int64_t(targetMs))
	if ret < 0 {
		return errors.New("seek failed")
	}
	return nil
}

// Flush flushes codec buffers (call after seek).
func (p *Player) Flush() {
	C.av_player_flush(p.c)
}