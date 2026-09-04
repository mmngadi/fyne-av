package av

// State represents the playback state of a Controller.
type State int

const (
	// StateStopped means playback is stopped (position reset to start).
	StateStopped State = iota
	// StatePlaying means playback is actively running.
	StatePlaying
	// StatePaused means playback is paused (position retained).
	StatePaused
)