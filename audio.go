package av

import (
	"encoding/binary"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/ebitengine/oto/v3"
)

// Global singleton oto context (oto forbids multiple contexts).
var (
	otoCtx    *oto.Context
	otoInitMu sync.Mutex
)

// audioFormat is the fixed output format sent to oto.
// Must match the C decoder (see internal/ffmpeg/ffmpeg.c OUT_* defines).
const (
	audioSampleRate = 44100
	audioChannels   = 2
	audioBytesPerMs = audioSampleRate * audioChannels * 2 / 1000 // 176.4
)

// errStaleGen is returned by pcmRing.write when the ring was reset while
// the writer was blocked. The caller must discard those bytes.
var errStaleGen = errors.New("stale generation")

func getOtoContext() (*oto.Context, error) {
	otoInitMu.Lock()
	defer otoInitMu.Unlock()
	if otoCtx != nil {
		return otoCtx, nil
	}
	var err error
	var ready chan struct{}
	otoCtx, ready, err = oto.NewContext(&oto.NewContextOptions{
		SampleRate:   audioSampleRate,
		ChannelCount: audioChannels,
		Format:       oto.FormatSignedInt16LE,
	})
	if err != nil {
		otoCtx = nil
		return nil, err
	}
	<-ready
	return otoCtx, nil
}

// pcmRing is a goroutine-safe byte buffer feeding PCM samples to oto.
// It also acts as the audio playback clock: written bytes minus what is
// still buffered (ring + oto) equals what the hardware has actually played.
type pcmRing struct {
	mu        sync.Mutex
	buf       []byte
	closed    bool
	written   int64 // total bytes ever written (monotonic clock)
	drained   int64 // total bytes consumed by oto readers
	cond      *sync.Cond
	otoBuffer int // last known oto internal buffer size
	gen       uint64 // bumped by reset: aborts writes that started before it
}

func newPCMRing() *pcmRing {
	r := &pcmRing{}
	r.cond = sync.NewCond(&r.mu)
	return r
}

func (r *pcmRing) write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Cap buffer at ~300ms to prevent unbounded growth / latency.
	maxBuf := int(300 * audioBytesPerMs)
	myGen := r.gen
	for len(r.buf)+len(p) > maxBuf && !r.closed {
		r.cond.Wait()
		if r.gen != myGen {
			// The buffer was reset (seek/stop) while we were blocked —
			// these bytes belong to a discarded playback epoch.
			return 0, errStaleGen
		}
	}
	if r.closed {
		return 0, io.ErrClosedPipe
	}
	if r.gen != myGen {
		return 0, errStaleGen
	}
	r.buf = append(r.buf, p...)
	r.written += int64(len(p))
	r.cond.Broadcast()
	return len(p), nil
}

func (r *pcmRing) Read(p []byte) (int, error) {
	r.mu.Lock()
	for len(r.buf) == 0 && !r.closed {
		r.cond.Wait()
	}
	if r.closed && len(r.buf) == 0 {
		r.mu.Unlock()
		return 0, io.EOF
	}
	n := copy(p, r.buf)
	r.buf = r.buf[n:]
	r.drained += int64(n)
	r.cond.Broadcast()
	r.mu.Unlock()
	return n, nil
}

// setOtoBuffered records oto's current internal buffer level so the
// playback clock can subtract it.
func (r *pcmRing) setOtoBuffered(n int) {
	r.mu.Lock()
	r.otoBuffer = n
	r.mu.Unlock()
}

// rawPlayedBytes returns the unfiltered hardware position in bytes:
// everything written minus what is still queued (ring + oto internal).
func (r *pcmRing) rawPlayedBytes() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	queued := int64(len(r.buf)) + int64(r.otoBuffer)
	pos := r.written - queued
	if pos < 0 {
		pos = 0
	}
	return pos
}

// reset clears all buffered audio and rewinds the clock. Used on
// Stop/Seek so stale audio doesn't leak into the next playback. Any
// writer blocked on a full buffer is aborted via the generation bump.
func (r *pcmRing) reset() {
	r.mu.Lock()
	r.buf = r.buf[:0]
	r.closed = false
	r.written = 0
	r.drained = 0
	r.otoBuffer = 0
	r.gen++
	r.cond.Broadcast()
	r.mu.Unlock()
}

func (r *pcmRing) close() {
	r.mu.Lock()
	r.closed = true
	r.cond.Broadcast()
	r.mu.Unlock()
}

func (r *pcmRing) available() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.buf)
}

// pcmScaler wraps a reader of S16LE PCM and applies software volume + mute.
// oto's player volume is left at 1.0; all scaling is done here per PRD.
type pcmScaler struct {
	r     *pcmRing
	mu    sync.Mutex
	vol   float64
	muted bool
}

func newPCMRScaler(r *pcmRing) *pcmScaler {
	return &pcmScaler{r: r, vol: 1.0}
}

func (s *pcmScaler) SetVolume(v float64) {
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	s.mu.Lock()
	s.vol = v
	s.mu.Unlock()
}

func (s *pcmScaler) Volume() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.vol
}

func (s *pcmScaler) SetMuted(m bool) {
	s.mu.Lock()
	s.muted = m
	s.mu.Unlock()
}

func (s *pcmScaler) Muted() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.muted
}

func (s *pcmScaler) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if n > 0 {
		s.mu.Lock()
		vol := s.vol
		muted := s.muted
		s.mu.Unlock()
		scalePCM(p[:n], vol, muted)
	}
	return n, err
}

// otoPlayer wraps an oto Player so it can be started/stopped with the controller.
type otoPlayer struct {
	ctx    *oto.Context
	player *oto.Player
	ring   *pcmRing
	scaler *pcmScaler

	// Clock smoothing state (see playedMs).
	clockMu  sync.Mutex
	lastRaw  int64     // last raw played-bytes estimate
	lastAt   time.Time // when lastRaw was taken
}

func newOtoPlayer() (*otoPlayer, error) {
	ctx, err := getOtoContext()
	if err != nil {
		return nil, err
	}
	ring := newPCMRing()
	scaler := newPCMRScaler(ring)
	player := ctx.NewPlayer(scaler)
	player.SetBufferSize(0) // use oto default — let it manage its own buffer
	return &otoPlayer{ctx: ctx, player: player, ring: ring, scaler: scaler}, nil
}

func (o *otoPlayer) Play()  { o.player.Play() }
func (o *otoPlayer) Pause() { o.player.Pause() }

// resetClock clears the smoothing filter so the next playedMs resyncs
// from a fresh raw sample. Call after ring.reset() (seek/stop/loop).
func (o *otoPlayer) resetClock() {
	o.clockMu.Lock()
	o.lastRaw = 0
	o.lastAt = time.Time{}
	o.clockMu.Unlock()
}

// playedMs reports the hardware playback position (the audio clock).
//
// BufferedSize() reflects driver bookkeeping that updates in coarse steps
// (especially in Android emulators' software audio pipes). A naive read
// makes the clock jump forward in bursts; every jump instantly marks all
// queued video frames "late" and the pacer drops them — capping playback
// at well below the source frame rate.
//
// The filter: hardware consumes samples linearly, so between bookkeeping
// updates the true position advances with wall time. We extrapolate from
// the last sample, but never beyond a fresh raw reading plus the elapsed
// time — keeping the estimate honest while removing measurement jitter.
func (o *otoPlayer) playedMs() int64 {
	raw := o.rawPlayedBytes()

	o.clockMu.Lock()
	defer o.clockMu.Unlock()
	now := time.Now()

	if o.player.IsPlaying() && !o.lastAt.IsZero() {
		elapsedMs := now.Sub(o.lastAt).Milliseconds()
		est := o.lastRaw + int64(float64(elapsedMs)*audioBytesPerMs)
		// Clamp: can't have consumed more than the elapsed-time budget
		// above the freshest raw sample.
		maxEst := raw + int64(float64(elapsedMs)*audioBytesPerMs)
		if est > maxEst {
			est = maxEst
		}
		if est < raw {
			est = raw // raw went ahead of our estimate — resync
		}
		o.lastRaw = est
	} else {
		o.lastRaw = raw
	}
	o.lastAt = now

	pos := o.lastRaw
	if pos < 0 {
		pos = 0
	}
	return int64(float64(pos) / audioBytesPerMs)
}

// rawPlayedBytes computes the unfiltered hardware position in bytes:
// everything written minus what is still queued (ring + oto internal).
func (o *otoPlayer) rawPlayedBytes() int64 {
	o.ring.setOtoBuffered(o.player.BufferedSize())
	return o.ring.rawPlayedBytes()
}

func (o *otoPlayer) Close() error {
	o.player.Pause()
	o.ring.close()
	return nil
}

func (o *otoPlayer) SetVolume(v float64) { o.scaler.SetVolume(v) }
func (o *otoPlayer) Volume() float64     { return o.scaler.Volume() }
func (o *otoPlayer) SetMuted(m bool)     { o.scaler.SetMuted(m) }
func (o *otoPlayer) Muted() bool         { return o.scaler.Muted() }
func (o *otoPlayer) Ring() *pcmRing      { return o.ring }

// scalePCM applies volume scaling to S16LE PCM samples in-place.
func scalePCM(buf []byte, vol float64, muted bool) {
	if muted {
		for i := range buf {
			buf[i] = 0
		}
		return
	}
	if vol >= 1.0 {
		return // fast path: no scaling needed
	}
	for i := 0; i+1 < len(buf); i += 2 {
		s := int16(binary.LittleEndian.Uint16(buf[i:]))
		scaled := int32(float64(s) * vol)
		if scaled > 32767 {
			scaled = 32767
		}
		if scaled < -32768 {
			scaled = -32768
		}
		binary.LittleEndian.PutUint16(buf[i:], uint16(int16(scaled)))
	}
}