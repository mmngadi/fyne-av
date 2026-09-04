package av

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mmngadi/fyne-av/internal/ffmpeg"
	"github.com/mmngadi/fyne-av/internal/frame"
)

// playbackStats tracks where frames are lost in the pipeline. Used by
// debugging tools; read via Controller.Stats.
type playbackStats struct {
	decoded     atomic.Int64 // frames decoded from FFmpeg
	paceDrops   atomic.Int64 // dropped by the pacer (too late)
	epochDrops  atomic.Int64 // dropped due to seek/stop epoch mismatch
	aheadDrops  atomic.Int64 // dropped by ahead-cap (stale epoch safety)
	displayed   atomic.Int64 // frames pushed to the widget
}

// Stats returns a snapshot of pipeline counters for diagnostics.
func (c *Controller) Stats() (decoded, paceDrops, epochDrops, aheadDrops, displayed int64) {
	return c.stats.decoded.Load(), c.stats.paceDrops.Load(),
		c.stats.epochDrops.Load(), c.stats.aheadDrops.Load(), c.stats.displayed.Load()
}

// audioPacket tags a decoded audio frame with the playback epoch it
// belongs to. Frames from a previous epoch (before a seek/stop/loop
// restart) are discarded by the writer.
type audioPacket struct {
	epoch uint64
	frame ffmpeg.AudioFrame
}

// pacedVideo tags a video frame with the epoch it belongs to. The pacer
// discards frames from a stale epoch — without this, a frame decoded just
// before a backward seek (PTS far ahead of the new clock) would freeze
// the video pipeline until the clock caught up.
type pacedVideo struct {
	epoch uint64
	frame frame.Video
}

// Controller manages media decoding, playback state, and audio output.
//
// Synchronization design: when audio is present, the audio hardware is
// the master clock. The ring buffer counts every byte written and, by
// subtracting what is still queued (ring + oto internal buffer), yields
// the exact position the hardware has played. Video frames are displayed
// when that clock reaches their PTS. Without audio, a wall clock is used.
type Controller struct {
	mu     sync.Mutex
	cfg    config
	player *ffmpeg.Player
	oto    *otoPlayer

	state State
	pos   int64 // milliseconds

	startClock time.Time // wall-clock reference (video-only fallback)
	startPts   int64

	// Audio clock state.
	audioEpoch     uint64 // incremented on seek/stop/loop restart
	audioBase      int64  // media PTS (ms) of first audio frame of this epoch
	audioBaseValid bool

	// Seek/stop handshake: the decoder parks between iterations so the
	// controller can operate the FFmpeg context without racing it.
	parkReq    chan struct{} // controller -> decoder: "park yourself"
	parkAck    chan struct{} // decoder -> controller: "I'm parked"
	parkRel    chan struct{} // controller -> decoder: "resume work"

	videoCh  chan frame.Video
	frameSig chan struct{}
	stopCh   chan struct{}
	chClosed bool

	stats playbackStats

	decodeWG    sync.WaitGroup
	audioWG     sync.WaitGroup
	videoWG     sync.WaitGroup
	audioCh     chan audioPacket
	videoPaceCh chan pacedVideo
	closed      bool
}

// NewController opens a media file and returns a Controller.
func NewController(src string, opts ...Option) (*Controller, error) {
	cfg := defaultConfig()
	for _, o := range opts {
		o(cfg)
	}

	fp, err := ffmpeg.Open(src, cfg.mode.cval())
	if err != nil {
		return nil, err
	}

	c := &Controller{
		cfg:      *cfg,
		player:   fp,
		state:    StateStopped,
		videoCh:  make(chan frame.Video, 2),
		frameSig: make(chan struct{}, 1),
		stopCh:   make(chan struct{}),
		parkReq:  make(chan struct{}, 1),
		parkAck:  make(chan struct{}, 1),
		parkRel:  make(chan struct{}, 1),
	}

	if fp.HasAudio() {
		op, err := newOtoPlayer()
		if err == nil {
			c.oto = op
			op.SetVolume(cfg.volume)
			op.SetMuted(cfg.muted)
		}
	}

	return c, nil
}

// Play starts or resumes playback.
func (c *Controller) Play() {
	c.mu.Lock()
	if c.state == StatePlaying || c.closed {
		c.mu.Unlock()
		return
	}
	prev := c.state
	c.state = StatePlaying
	if c.oto != nil {
		c.oto.Play()
	}
	if prev == StateStopped {
		c.stopCh = make(chan struct{})
		c.chClosed = false
		c.startClock = time.Time{}
		c.startPts = 0
		c.startDecodeLoop()
	}
	c.mu.Unlock()
	if c.cfg.onState != nil {
		c.cfg.onState(StatePlaying)
	}
}

// Pause suspends playback.
func (c *Controller) Pause() {
	c.mu.Lock()
	if c.state != StatePlaying {
		c.mu.Unlock()
		return
	}
	c.state = StatePaused
	if c.oto != nil {
		c.oto.Pause()
	}
	c.mu.Unlock()
	if c.cfg.onState != nil {
		c.cfg.onState(StatePaused)
	}
}

// stopLoops signals all goroutines to exit and waits for them.
// Caller must NOT hold c.mu.
func (c *Controller) stopLoops() {
	c.mu.Lock()
	if !c.chClosed {
		close(c.stopCh)
		c.chClosed = true
	}
	if c.oto != nil {
		c.oto.Pause()
		c.oto.Ring().close()
	}
	c.mu.Unlock()

	// Release a parked decoder so it can observe stopCh and exit.
	select {
	case c.parkRel <- struct{}{}:
	default:
	}
	// Drain any pending park request so it doesn't affect the next run.
	select {
	case <-c.parkReq:
	default:
	}

	c.decodeWG.Wait()
	c.audioWG.Wait()
	c.videoWG.Wait()
}

// parkDecoder asks the decode loop to pause at a safe point (between
// NextFrame calls) and waits until it confirms. While parked, it is safe
// to call SeekTo/Flush on the FFmpeg context from this goroutine.
// Returns false if the decode loop is not running.
func (c *Controller) parkDecoder(timeout time.Duration) bool {
	c.parkReq <- struct{}{}
	select {
	case <-c.parkAck:
		return true
	case <-time.After(timeout):
		// Decoder didn't respond (stopping or wedged): undo the request.
		select {
		case <-c.parkReq:
		default:
		}
		return false
	}
}

// unparkDecoder releases the parked decoder back to work.
func (c *Controller) unparkDecoder() {
	c.parkRel <- struct{}{}
}

// Stop halts playback and resets position to start.
func (c *Controller) Stop() {
	c.mu.Lock()
	if c.state == StateStopped {
		c.mu.Unlock()
		return
	}
	c.state = StateStopped
	c.mu.Unlock()

	c.stopLoops()

	c.mu.Lock()
	c.pos = 0
	c.audioEpoch++
	c.audioBaseValid = false
	// Loops are stopped: FFmpeg access is safe without parking.
	c.player.SeekTo(0)
	c.player.Flush()
	if c.oto != nil {
		c.oto.Ring().reset(); c.oto.resetClock()
	}
drainLoop:
	for {
		select {
		case <-c.videoCh:
		case <-c.videoPaceCh:
		default:
			break drainLoop
		}
	}
	c.mu.Unlock()

	if c.cfg.onState != nil {
		c.cfg.onState(StateStopped)
	}
}

// Seek seeks to the target time.
func (c *Controller) Seek(target time.Duration) error {
	c.mu.Lock()
	if c.player == nil {
		c.mu.Unlock()
		return errors.New("no media loaded")
	}
	wasPlaying := c.state == StatePlaying
	parked := false
	// Decode loop parks itself whenever state != Playing.
	if c.state == StatePlaying {
		c.state = StatePaused
		c.mu.Unlock()
		parked = c.parkDecoder(2 * time.Second)
		if !parked {
			// Fall back to stopping everything before rewinding.
			c.stopLoops()
		}
	} else {
		c.mu.Unlock()
	}

	c.mu.Lock()
	// Invalidate everything queued before the seek.
	c.audioEpoch++
	c.audioBaseValid = false
	if c.oto != nil {
		c.oto.Pause()
		c.oto.Ring().reset(); c.oto.resetClock()
	}
drainLoop:
	for {
		select {
		case <-c.videoCh:
		case <-c.videoPaceCh:
		default:
			break drainLoop
		}
	}
	targetMs := target.Milliseconds()
	if err := c.player.SeekTo(targetMs); err != nil {
		c.startClock = time.Time{}
		c.startPts = 0
		if wasPlaying {
			c.state = StatePlaying
		}
		c.mu.Unlock()
		if parked {
			c.unparkDecoder()
		}
		return err
	}
	c.player.Flush()
	c.pos = targetMs
	c.startClock = time.Time{}
	c.startPts = 0
	if wasPlaying {
		c.state = StatePlaying
	}
	if c.oto != nil && wasPlaying {
		c.oto.Play()
	}
	c.mu.Unlock()

	if parked {
		c.unparkDecoder()
	} else if wasPlaying {
		// Loops were stopped as a fallback: relaunch them.
		c.mu.Lock()
		c.stopCh = make(chan struct{})
		c.chClosed = false
		c.mu.Unlock()
		c.startDecodeLoop()
	}
	return nil
}

// Forward seeks forward by the given duration.
func (c *Controller) Forward(d time.Duration) error {
	return c.Seek(time.Duration(c.Position()) + d)
}

// Rewind seeks backward by the given duration.
func (c *Controller) Rewind(d time.Duration) error {
	return c.Seek(time.Duration(c.Position()) - d)
}

// SetLoop enables or disables looping.
func (c *Controller) SetLoop(loop bool) {
	c.mu.Lock()
	c.cfg.loop = loop
	c.mu.Unlock()
}

// Loop returns the current loop setting.
func (c *Controller) Loop() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cfg.loop
}

// SetVolume sets the software volume (0.0 to 1.0).
func (c *Controller) SetVolume(v float64) {
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	c.mu.Lock()
	c.cfg.volume = v
	if c.oto != nil {
		c.oto.SetVolume(v)
	}
	c.mu.Unlock()
}

// Volume returns the current volume.
func (c *Controller) Volume() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cfg.volume
}

// SetMuted mutes or unmutes audio.
func (c *Controller) SetMuted(m bool) {
	c.mu.Lock()
	c.cfg.muted = m
	if c.oto != nil {
		c.oto.SetMuted(m)
	}
	c.mu.Unlock()
}

// Muted returns the current mute state.
func (c *Controller) Muted() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cfg.muted
}

// State returns the current playback state.
func (c *Controller) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

// Duration returns the total media duration.
func (c *Controller) Duration() time.Duration {
	if c.player == nil {
		return 0
	}
	return time.Duration(c.player.DurationMs()) * time.Millisecond
}

// Position returns the current playback position, derived from the
// audio hardware clock when audio is playing (exact), otherwise the
// PTS of the most recently processed frame.
func (c *Controller) Position() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.oto != nil && c.audioBaseValid && c.state == StatePlaying {
		return c.oto.playedMs() + c.audioBase
	}
	return c.pos
}

// HasVideo returns whether the media has a video stream being decoded.
func (c *Controller) HasVideo() bool {
	if c.player == nil {
		return false
	}
	return c.player.HasVideo()
}

// HasAudio returns whether the media has an audio stream being decoded.
func (c *Controller) HasAudio() bool {
	if c.player == nil {
		return false
	}
	return c.player.HasAudio()
}

// FrameSignal returns a channel that receives a signal on each new video frame.
func (c *Controller) FrameSignal() <-chan struct{} {
	return c.frameSig
}

// VideoFrame returns the latest video frame from the channel (non-blocking).
func (c *Controller) VideoFrame() (frame.Video, bool) {
	select {
	case f := <-c.videoCh:
		return f, true
	default:
		return frame.Video{}, false
	}
}

// Close releases all resources.
func (c *Controller) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	if c.state == StatePlaying || c.state == StatePaused {
		c.mu.Unlock()
		c.Stop()
		c.mu.Lock()
	}
	if c.oto != nil {
		c.oto.Close()
	}
	if c.player != nil {
		c.player.Close()
	}
	c.mu.Unlock()
	return nil
}

// startDecodeLoop launches the decode, video-pacer, and audio-writer
// goroutines. The decoder never blocks on pacing: video frames are queued
// to a small paced channel consumed by videoPaceLoop, and audio packets
// flow to the writer which drains at the hardware's real-time rate.
func (c *Controller) startDecodeLoop() {
	c.audioCh = make(chan audioPacket, 4)
	c.videoPaceCh = make(chan pacedVideo, 64)
	c.decodeWG.Add(1)
	go c.decodeLoop()
	c.videoWG.Add(1)
	go c.videoPaceLoop()
	if c.oto != nil {
		c.audioWG.Add(1)
		go c.audioWriteLoop()
	}
}

// videoPaceLoop receives decoded video frames and displays each when the
// master clock reaches its PTS. Running in its own goroutine is essential:
// pacing sleeps never stall the decoder, which must keep producing audio
// or the audio clock (the master) would freeze.
func (c *Controller) videoPaceLoop() {
	defer c.videoWG.Done()
	for {
		select {
		case <-c.stopCh:
			return
		case pv, ok := <-c.videoPaceCh:
			if !ok {
				return
			}
			// Discard frames queued before the latest seek/stop/loop.
			c.mu.Lock()
			curEpoch := c.audioEpoch
			c.mu.Unlock()
			if pv.epoch != curEpoch {
				c.stats.epochDrops.Add(1)
				continue
			}
			if !c.paceVideo(pv.frame.PtsMs) {
				c.stats.paceDrops.Add(1)
				continue // frame dropped (too late / stopping)
			}
			// Epoch may have changed while pacing — re-validate.
			c.mu.Lock()
			still := pv.epoch == c.audioEpoch
			c.mu.Unlock()
			if !still {
				c.stats.epochDrops.Add(1)
				continue
			}
			c.mu.Lock()
			c.pos = pv.frame.PtsMs
			c.mu.Unlock()
			select {
			case c.videoCh <- pv.frame:
			default:
				// Channel full: drop the oldest queued frame to make room.
				select {
				case <-c.videoCh:
				default:
				}
				c.videoCh <- pv.frame
			}
			c.stats.displayed.Add(1)
			select {
			case c.frameSig <- struct{}{}:
			default:
			}
		}
	}
}

// audioWriteLoop converts audio frames to PCM bytes and feeds the oto
// ring buffer. It runs in its own goroutine: blocking on a full ring
// throttles audio relative to the hardware without stalling the decoder.
func (c *Controller) audioWriteLoop() {
	defer c.audioWG.Done()
	for {
		select {
		case <-c.stopCh:
			return
		case pkt, ok := <-c.audioCh:
			if !ok {
				return
			}
			c.mu.Lock()
			curEpoch := c.audioEpoch
			baseValid := c.audioBaseValid
			c.mu.Unlock()

			// Discard frames queued before the latest seek/stop/loop restart.
			if pkt.epoch != curEpoch {
				continue
			}

			c.mu.Lock()
			ring := c.oto.Ring()
			c.mu.Unlock()
			if ring == nil {
				continue
			}

			buf := make([]byte, len(pkt.frame.PCM)*2)
			for i, s := range pkt.frame.PCM {
				buf[2*i] = byte(s)
				buf[2*i+1] = byte(s >> 8)
			}
			if _, err := ring.write(buf); err != nil {
				if err == errStaleGen {
					continue // ring was reset mid-write (seek): drop bytes
				}
				return
			}

			// The first frame written in this epoch anchors the audio
			// clock to media time.
			if !baseValid {
				c.mu.Lock()
				if !c.audioBaseValid && pkt.epoch == c.audioEpoch {
					c.audioBase = pkt.frame.PtsMs
					c.audioBaseValid = true
				}
				c.mu.Unlock()
			}
		}
	}
}

// audioClockMs returns the media-time position the audio hardware has
// reached, and whether the clock is usable.
func (c *Controller) audioClockMs() (int64, bool) {
	c.mu.Lock()
	valid := c.audioBaseValid
	base := c.audioBase
	c.mu.Unlock()
	if !valid || c.oto == nil {
		return 0, false
	}
	return c.oto.playedMs() + base, true
}

// paceVideo blocks until the frame with the given PTS is due for display.
// Returns false if the frame should be dropped (too late, absurdly far
// ahead, or playback stopped). The ahead-cap is a safety net: a frame
// from a stale epoch could otherwise park the pacer for minutes.
//
// Scheduling strategy: sleep the EXACT time until the frame is due (plus
// a small fudge for coarse OS timers — Android emulators frequently
// oversleep or undersleep by several ms). Polling at a fixed tick
// quantizes display times to the tick interval, which capped the
// emulator at ~16fps for 24fps content.
func (c *Controller) paceVideo(ptsMs int64) (due bool) {
	const (
		lateThreshold = 250 * time.Millisecond // drop frames later than this
		aheadCap      = 1 * time.Second        // never wait longer than this
		fudge         = 2 * time.Millisecond    // extra sleep for timer slack
	)

	for {
		select {
		case <-c.stopCh:
			return false
		default:
		}

		// Master clock: audio hardware position when available.
		if clockMs, ok := c.audioClockMs(); ok {
			ahead := time.Duration(ptsMs-clockMs) * time.Millisecond
			if ahead > aheadCap {
				c.stats.aheadDrops.Add(1)
				return false // stale epoch frame — drop instead of parking
			}
			if ahead <= 0 {
				// Due now. Very late frames are dropped entirely.
				return ahead >= -lateThreshold
			}
			// Exactly due in `ahead`: sleep it in one shot. Cap each
			// sleep at aheadCap so stopCh stays responsive, and only
			// add fudge when the wait is long enough to absorb it.
			wait := ahead
			if wait > aheadCap {
				wait = aheadCap
			} else if wait > fudge {
				wait -= fudge
			}
			select {
			case <-c.stopCh:
				return false
			case <-time.After(wait):
			}
			continue // re-check the clock; loop until due
		}

		// Fallback: wall clock (video-only mode, or before first audio).
		c.mu.Lock()
		if c.startClock.IsZero() {
			c.startClock = time.Now()
			c.startPts = ptsMs
		}
		startClock := c.startClock
		startPts := c.startPts
		c.mu.Unlock()

		elapsed := time.Since(startClock)
		frameTime := time.Duration(ptsMs-startPts) * time.Millisecond
		if frameTime <= elapsed {
			return true
		}
		wait := frameTime - elapsed
		if wait > aheadCap {
			wait = aheadCap
		} else if wait > fudge {
			wait -= fudge
		}
		select {
		case <-c.stopCh:
			return false
		case <-time.After(wait):
		}
	}
}

// decodeLoop is the single-threaded FFmpeg decode loop. It decodes as
// fast as the container allows; video frames are paced by paceVideo and
// audio frames are handed to the writer goroutine.
//
// Park protocol: between iterations the loop checks seekReq. When a
// seek arrives it confirms with seekAck and then idles (without touching
// FFmpeg) until the controller releases it, guaranteeing SeekTo/Flush
// never race a concurrent NextFrame.
func (c *Controller) decodeLoop() {
	defer c.decodeWG.Done()
	for {
		// Seek handshake: park if the controller asked us to.
		select {
		case <-c.stopCh:
			return
		case <-c.parkReq:
			c.parkAck <- struct{}{} // "parked — FFmpeg is yours"
			<-c.parkRel             // idle until released
		default:
		}

		c.mu.Lock()
		isPlaying := c.state == StatePlaying
		epoch := c.audioEpoch
		c.mu.Unlock()
		if !isPlaying {
			select {
			case <-c.stopCh:
				return
			case <-time.After(50 * time.Millisecond):
			}
			continue
		}

		// Snapshot the epoch BEFORE decoding: any frame produced by this
		// NextFrame call belongs to it, even if a seek lands mid-decode.
		fr, err := c.player.NextFrame()
		if err != nil {
			if c.handleEOF() {
				continue // loop mode: playback restarted
			}
			return
		}

		switch fr.Type {
		case ffmpeg.FrameVideo:
			// Non-blocking send; if the pace queue is full the incoming
			// (newest) frame is dropped. Keeping the queued frames in PTS
			// order preserves display continuity — the pacer drains the
			// queue at display rate.
			vf := frame.Video{
				RGBA:   fr.Video.RGBA,
				Width:  fr.Video.Width,
				Height: fr.Video.Height,
				PtsMs:  fr.Video.PtsMs,
			}
			c.stats.decoded.Add(1)
			select {
			case c.videoPaceCh <- pacedVideo{epoch: epoch, frame: vf}:
			default:
				// queue full — drop this frame
			}

		case ffmpeg.FrameAudio:
			c.mu.Lock()
			c.pos = fr.Audio.PtsMs
			c.mu.Unlock()
			// Blocking send: audio frames must never be dropped, or the
			// byte-count audio clock would lose continuity. This cannot
			// stall playback: the writer drains independently into a ring
			// that oto consumes at real-time.
			select {
			case c.audioCh <- audioPacket{epoch: epoch, frame: fr.Audio}:
			case <-c.stopCh:
				return
			}
		}
	}
}

// handleEOF is called when decoding reaches end-of-stream. It returns
// true when playback should continue (loop mode), false when it stops.
func (c *Controller) handleEOF() bool {
	c.mu.Lock()
	if c.cfg.loop {
		c.player.SeekTo(0)
		c.player.Flush()
		c.pos = 0
		c.audioEpoch++
		c.audioBaseValid = false
		if c.oto != nil {
			c.oto.Ring().reset(); c.oto.resetClock()
		}
		// Drop frames queued from the previous loop iteration.
	pacedrain:
		for {
			select {
			case <-c.videoPaceCh:
			case <-c.videoCh:
			default:
				break pacedrain
			}
		}
		c.mu.Unlock()
		return true
	}

	// Non-loop: transition to stopped and wake every goroutine.
	c.state = StateStopped
	if !c.chClosed {
		close(c.stopCh)
		c.chClosed = true
	}
	if c.oto != nil {
		c.oto.Pause()
	}
	onState := c.cfg.onState
	onEOF := c.cfg.onEOF
	c.mu.Unlock()

	if onState != nil {
		onState(StateStopped)
	}
	if onEOF != nil {
		onEOF()
	}
	return false
}