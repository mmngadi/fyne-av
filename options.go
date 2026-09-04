package av

// Option configures a Controller at creation time.
type Option func(*config)

type config struct {
	mode      Mode
	loop      bool
	volume    float64
	muted     bool
	onEOF     func()
	onState   func(State)
}

func defaultConfig() *config {
	return &config{
		mode:   ModeAuto,
		loop:   false,
		volume: 1.0,
		muted:  false,
	}
}

// WithMode sets the stream decoding mode.
func WithMode(m Mode) Option {
	return func(c *config) { c.mode = m }
}

// WithLoop enables or disables looping playback (restart on EOF).
func WithLoop(loop bool) Option {
	return func(c *config) { c.loop = loop }
}

// WithVolume sets the initial software volume (0.0 to 1.0).
func WithVolume(v float64) Option {
	return func(c *config) {
		if v < 0 {
			v = 0
		}
		if v > 1 {
			v = 1
		}
		c.volume = v
	}
}

// WithMuted sets the initial mute state.
func WithMuted(m bool) Option {
	return func(c *config) { c.muted = m }
}

// WithOnEOF sets a callback invoked when playback reaches end-of-file (non-loop mode).
func WithOnEOF(fn func()) Option {
	return func(c *config) { c.onEOF = fn }
}

// WithOnStateChange sets a callback invoked on each state transition.
func WithOnStateChange(fn func(State)) Option {
	return func(c *config) { c.onState = fn }
}