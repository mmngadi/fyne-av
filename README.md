# fyne-av

A lightweight, cross-platform audio and video library for [Fyne](https://fyne.io/) applications.

`fyne-av` provides low-level media primitives — a thread-safe media controller and a frame-rendering widget — allowing you to integrate video and audio playback into Fyne apps without external runtime dependencies (`.so`, `.dll`). All FFmpeg decoding is statically compiled into your binary.

## Features

- **Zero external runtime deps** — FFmpeg is statically linked (`.a` archives), no system FFmpeg required
- **Primitive-first** — provides `av.Controller` and `av.VideoPlayer`; you build the UI
- **Multi-mode** — Video+Audio, Video-Only, Audio-Only
- **Audio-synced video** — the audio hardware clock is the master; video frames are paced and dropped to match exactly
- **Aspect-ratio aware** — the VideoPlayer letterboxes video into any container without stretching

## Platform Support

| Platform | Status |
|----------|--------|
| **Linux Desktop** (amd64) | Tested and working |
| **Android** (arm64-v8a, amd64) | Tested on Android emulator |
| **Windows** (amd64) | Builds via MinGW-w64 cross-compiler — needs on-device testing |
| **macOS** (amd64, arm64) | Help wanted — needs FFmpeg static build for macOS |
| **iOS** (arm64) | Help wanted — needs FFmpeg xcframework compilation |

> **Help Wanted:** I don't have a macOS device to compile FFmpeg static libraries for macOS and iOS. If you have a Mac and can contribute the FFmpeg build scripts (see [BUILD.md](BUILD.md) → "macOS / iOS") and test on those platforms, please open a PR or issue.

## Install

```bash
go get github.com/mmngadi/fyne-av
```

Because `fyne-av` statically links FFmpeg via cgo, `go get` only fetches the Go source — the prebuilt FFmpeg archives are **not** in the module. After `go get`, fetch the static libraries for your target platform:

```bash
go run github.com/mmngadi/fyne-av/cmd/fetch-libs
```

This downloads the correct FFmpeg `.a` archives from the [GitHub Releases](https://github.com/mmngadi/fyne-av/releases) page into the module's `libs/` directory so cgo can find them at build time. For cross-compiling (e.g. Android), set `FYNE_AV_TARGET_GOOS`/`FYNE_AV_TARGET_GOARCH` (not `GOOS`/`GOARCH`, which would cross-compile the fetcher itself):

```bash
FYNE_AV_TARGET_GOOS=android FYNE_AV_TARGET_GOARCH=arm64 go run github.com/mmngadi/fyne-av/cmd/fetch-libs
FYNE_AV_TARGET_GOOS=android FYNE_AV_TARGET_GOARCH=amd64 go run github.com/mmngadi/fyne-av/cmd/fetch-libs
FYNE_AV_TARGET_GOOS=windows FYNE_AV_TARGET_GOARCH=amd64 go run github.com/mmngadi/fyne-av/cmd/fetch-libs
```

If you prefer to build FFmpeg from source yourself (e.g. to customize decoders), see [BUILD.md](BUILD.md).

## Quick Start

```go
package main

import (
	av "github.com/mmngadi/fyne-av"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
)

func main() {
	a := app.New()
	w := a.NewWindow("Video Player")

	ctrl, _ := av.NewController("video.mp4", av.WithMode(av.ModeAuto))
	player := av.NewVideoPlayer(ctrl)
	player.SetAspectRatio(av.Aspect16_9)

	w.SetContent(container.NewStack(player))
	w.Resize(fyne.NewSize(800, 600))

	ctrl.Play()
	w.ShowAndRun()
	ctrl.Close()
}
```

## Usage Guide

### 1. Create a Controller

The `Controller` manages media decoding, playback state, and audio output. Pass a file path and optional configuration:

```go
ctrl, err := av.NewController("path/to/video.mp4",
	av.WithMode(av.ModeVideoAndAudio),
	av.WithLoop(true),
	av.WithVolume(0.8),
)
if err != nil {
	// handle error
}
defer ctrl.Close()
```

### 2. Create a VideoPlayer Widget

The `VideoPlayer` is a Fyne widget that renders decoded video frames. Place it in any Fyne container:

```go
player := av.NewVideoPlayer(ctrl)
player.SetAspectRatio(av.Aspect16_9) // letterbox to 16:9
```

For audio-only playback, the widget renders a solid black background.

### 3. Control Playback

```go
ctrl.Play()              // start or resume
ctrl.Pause()             // suspend (position retained)
ctrl.Stop()              // stop and reset to beginning
ctrl.Seek(30 * time.Second)  // jump to 30s
ctrl.Forward(10 * time.Second)
ctrl.Rewind(10 * time.Second)
```

### 4. Volume & Mute

Volume is applied in software on the PCM audio buffer (does not touch the OS master volume):

```go
ctrl.SetVolume(0.5)  // 50% volume
ctrl.SetMuted(true)  // mute
```

### 5. Looping

```go
ctrl.SetLoop(true)  // restart from beginning on EOF
// or at creation: av.WithLoop(true)
```

### 6. Callbacks

```go
ctrl, _ := av.NewController("video.mp4",
	av.WithOnEOF(func() {
		fmt.Println(" playback finished")
	}),
	av.WithOnStateChange(func(s av.State) {
		fmt.Println("state:", s)
	}),
)
```

### 7. Aspect Ratio

The `VideoPlayer` preserves the video's aspect ratio by letterboxing (black bars). Choose a preset or let it auto-detect from the decoded frame:

```go
player.SetAspectRatio(av.Aspect16_9)  // 1.778
player.SetAspectRatio(av.Aspect4_3)   // 1.333
player.SetAspectRatio(av.Aspect21_9)  // 2.370 (cinematic)
player.SetAspectRatio(av.Aspect1_1)   // square
player.SetAspectRatio(av.AspectAuto)  // derive from frame (default)
```

### 8. Audio-Only Mode

For music players, use `ModeAudioOnly`. The `VideoPlayer` renders a solid background:

```go
ctrl, _ := av.NewController("song.mp3", av.WithMode(av.ModeAudioOnly))
player := av.NewVideoPlayer(ctrl) // shows black background
ctrl.Play()
```

### 9. Position & Duration

```go
pos := ctrl.Position()     // current position in milliseconds
dur := ctrl.Duration()     // total duration
fmt.Printf("%v / %v\n", time.Duration(pos)*time.Millisecond, dur)
```

Position is derived from the audio hardware clock when audio is playing — it's exact. For video-only or paused state, it reflects the last displayed frame's PTS.

## Supported Formats

**Video Decoders:** H.264, H.265/HEVC, VP8, VP9, AV1, MPEG-4, MPEG-2, Theora

**Audio Decoders:** AAC, MP3, FLAC, Opus, Vorbis, PCM (S16/S24/F32), ALAC

**Containers:** MP4/MOV, Matroska/MKV, WebM, AVI, FLV, MP3, OGG, WAV, AAC, MPEG-TS

## Architecture

```
+--------------------------------------------------------------------+
|                         Developer Application                      |
|  +---------------------------+  +-------------------------------+  |
|  | Custom Fyne Controls (UI) |  |   av.VideoPlayer (Raster)     |  |
+-----------------|-------------------------------|------------------+
                  | Calls API                     | Draws Pixels
                  v                               v
+--------------------------------------------------------------------+
|                             fyne-av                                |
|  +--------------------------------------------------------------+  |
|  |                       av.Controller                          |  |
|  |  - Play / Pause / Seek / Volume / Mute / Loop State Machine    |  |
|  +------------------------------+-------------------------------+  |
|         +-----------------------+-----------------------+          |
|         | Video Stream                                  | Audio    |
|         v                                               v          |
|  [ libswscale → RGBA ]                          [ libswresample → 44100Hz S16 ]
|         |                                               |          |
|         v                                               v          |
|  [ Paced Frame Queue ]                          [ Oto Player ]     |
|  (audio clock = master)                         (hardware output)   |
+--------------------------------------------------------------------+
                  | Cgo Static Linking
                  v
+--------------------------------------------------------------------+
|                      Static Native Libraries                       |
|  libavcodec.a | libavformat.a | libswscale.a | libswresample.a ... |
+--------------------------------------------------------------------+
```

## How A/V Sync Works

When audio is present, the **audio hardware is the master clock**. The controller counts every byte written to the audio device and subtracts what's still buffered (ring + oto internal) to compute the exact playback position. Video frames are displayed when that clock reaches their PTS — they're never allowed to drift. Without audio (video-only mode), a wall clock is used instead.

## License

- `fyne-av` Go code: **MIT**
- FFmpeg static archives: **GPL** (built with `--enable-gpl`)

---

<details>
<summary><strong style="font-size: x-large">API Reference</strong></summary>

### Types

#### `Controller`

Manages media decoding, playback state, and audio output. Thread-safe.

#### `VideoPlayer`

A Fyne `widget.BaseWidget` that renders video frames via `canvas.Raster`. Letterboxes video to preserve the configured aspect ratio. In audio-only mode, renders a solid black background.

#### `Mode`

Stream decoding mode.

| Constant | Value | Description |
|----------|-------|-------------|
| `ModeAuto` | `0` | Detect and decode all available streams (default) |
| `ModeVideoAndAudio` | `1` | Decode both video and audio |
| `ModeVideoOnly` | `2` | Decode video; skip audio |
| `ModeAudioOnly` | `3` | Decode audio; skip video (for music files) |

#### `State`

Playback state.

| Constant | Value | Description |
|----------|-------|-------------|
| `StateStopped` | `0` | Stopped, position reset to beginning |
| `StatePlaying` | `1` | Actively playing |
| `StatePaused` | `2` | Paused, position retained |

#### `AspectRatio`

Controls how the VideoPlayer sizes the video within its allocated area.

| Constant | Value | Description |
|----------|-------|-------------|
| `AspectAuto` | `0` | Derive from decoded frame dimensions (default) |
| `Aspect16_9` | `1.778` | Standard widescreen |
| `Aspect4_3` | `1.333` | Classic TV |
| `Aspect21_9` | `2.370` | Cinematic |
| `Aspect1_1` | `1.0` | Square |

#### `Option`

Functional option for configuring a Controller at creation time.

### Functions

#### `NewController`

```go
func NewController(src string, opts ...Option) (*Controller, error)

```

Opens a media file at `src` (filesystem path) and returns a Controller configured by `opts`. Returns an error if the file cannot be opened or no decodable streams are found.

#### `NewVideoPlayer`

```go
func NewVideoPlayer(ctrl *Controller) *VideoPlayer

```

Creates a VideoPlayer widget bound to the given Controller. If `ctrl` is nil, creates an empty player (use `SetController` to bind later).

### Options

#### `WithMode`

```go
func WithMode(m Mode) Option

```

Sets the stream decoding mode. Default: `ModeAuto`.

#### `WithLoop`

```go
func WithLoop(loop bool) Option

```

Enables or disables looping playback (restart on EOF). Default: `false`.

#### `WithVolume`

```go
func WithVolume(v float64) Option

```

Sets the initial software volume. Range: 0.0 (silent) to 1.0 (full). Default: 1.0.

#### `WithMuted`

```go
func WithMuted(m bool) Option

```

Sets the initial mute state. Default: `false`.

#### `WithOnEOF`

```go
func WithOnEOF(fn func()) Option

```

Sets a callback invoked when playback reaches end-of-file (non-loop mode). Not called in loop mode.

#### `WithOnStateChange`

```go
func WithOnStateChange(fn func(State)) Option

```

Sets a callback invoked on each state transition (`StatePlaying`, `StatePaused`, `StateStopped`).

### Controller Methods

#### `Play`

```go
func (c *Controller) Play()

```

Starts or resumes playback. If resuming from pause, continues from the current position. If starting from stopped, begins from the beginning.

#### `Pause`

```go
func (c *Controller) Pause()

```

Suspends playback. The position is retained; call `Play()` to resume.

#### `Stop`

```go
func (c *Controller) Stop()

```

Stops playback, resets position to the beginning, and stops all decode goroutines. Call `Play()` to start again from the beginning.

#### `Seek`

```go
func (c *Controller) Seek(target time.Duration) error

```

Seeks to the target position. Playback continues if it was playing, or stays paused if paused.

#### `Forward`

```go
func (c *Controller) Forward(d time.Duration) error

```

Seeks forward by `d` from the current position.

#### `Rewind`

```go
func (c *Controller) Rewind(d time.Duration) error

```

Seeks backward by `d` from the current position.

#### `SetLoop`

```go
func (c *Controller) SetLoop(loop bool)

```

Enables or disables looping at runtime.

#### `Loop`

```go
func (c *Controller) Loop() bool

```

Returns the current loop setting.

#### `SetVolume`

```go
func (c *Controller) SetVolume(v float64)

```

Sets the software volume. Range: 0.0 to 1.0. Applied to PCM audio samples in Go — does not touch the OS master volume.

#### `Volume`

```go
func (c *Controller) Volume() float64

```

Returns the current volume (0.0 to 1.0).

#### `SetMuted`

```go
func (c *Controller) SetMuted(m bool)

```

Mutes or unmutes audio output.

#### `Muted`

```go
func (c *Controller) Muted() bool

```

Returns the current mute state.

#### `State`

```go
func (c *Controller) State() State

```

Returns the current playback state (`StateStopped`, `StatePlaying`, or `StatePaused`).

#### `Duration`

```go
func (c *Controller) Duration() time.Duration

```

Returns the total media duration.

#### `Position`

```go
func (c *Controller) Position() int64

```

Returns the current playback position in milliseconds. When audio is playing, this is derived from the audio hardware clock (exact). Otherwise, reflects the last displayed frame's PTS.

#### `HasVideo`

```go
func (c *Controller) HasVideo() bool

```

Returns true if the media has a video stream being decoded.

#### `HasAudio`

```go
func (c *Controller) HasAudio() bool

```

Returns true if the media has an audio stream being decoded.

#### `Close`

```go
func (c *Controller) Close() error

```

Releases all resources (FFmpeg context, audio device, goroutines). Must be called when the Controller is no longer needed. Safe to call multiple times.

### VideoPlayer Methods

#### `SetController`

```go
func (vp *VideoPlayer) SetController(ctrl *Controller)

```

Binds a new Controller to the player. Replaces any previous binding.

#### `SetAspectRatio`

```go
func (vp *VideoPlayer) SetAspectRatio(r AspectRatio)

```

Sets the aspect ratio the video area enforces. The video is letterboxed (centered with black bars) within the available space to preserve the ratio. Use `AspectAuto` to derive from the decoded frame.

#### `AspectRatio`

```go
func (vp *VideoPlayer) AspectRatio() AspectRatio

```

Returns the currently effective aspect ratio. When set to `AspectAuto` and a frame has been received, returns the frame's ratio.

### Controller Signals (for custom widgets)

These are used by `VideoPlayer` internally but are also available for developers building custom rendering:

#### `FrameSignal`

```go
func (c *Controller) FrameSignal() <-chan struct{}

```

Returns a channel that receives a signal each time a new video frame is ready to display.

#### `VideoFrame`

```go
func (c *Controller) VideoFrame() (frame.Video, bool)

```

Returns the latest video frame from the internal queue (non-blocking). Returns `false` if no frame is available.

</details>