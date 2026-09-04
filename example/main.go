package main

import (
	"fmt"
	"image/color"
	"io"
	"os"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	av "github.com/mmngadi/fyne-av"
)

func main() {
	a := app.New()
	w := a.NewWindow("Fyne VideoPlayer")

	var ctrl *av.Controller
	var vp *av.VideoPlayer
	var controls *overlayControls

	openFile := func() {
		dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
			if err != nil || reader == nil {
				return
			}
			go func() {
				tmpPath, copyErr := copyToTemp(reader)
				reader.Close()
				if copyErr != nil {
					fyne.Do(func() {
						dialog.ShowError(fmt.Errorf("failed to copy file: %w", copyErr), w)
					})
					return
				}
				fyne.Do(func() {
					openMedia(w, tmpPath, &ctrl, &vp, &controls)
				})
			}()
		}, w).Show()
	}

	vp = av.NewVideoPlayer(nil)
	vp.SetAspectRatio(av.Aspect16_9)
	controls = newOverlayControls(w, nil)

	// Separate Open File Button placed ABOVE the video player area
	topOpenBtn := widget.NewButtonWithIcon("Open File", theme.FolderOpenIcon(), openFile)
	topBar := container.NewHBox(topOpenBtn)

	tapOverlay := newInteractiveTapArea(func() {
		if ctrl == nil {
			return
		}
		if ctrl.State() == av.StatePlaying {
			ctrl.Pause()
		} else {
			ctrl.Play()
		}
	}, controls)

	videoArea := container.NewStack(vp, tapOverlay, controls.container)

	// Combine top bar and video area in main window content
	content := container.NewBorder(topBar, nil, nil, nil, videoArea)

	w.SetContent(content)
	w.Resize(fyne.NewSize(960, 540))

	w.SetOnClosed(func() {
		if ctrl != nil {
			ctrl.Close()
		}
	})

	w.ShowAndRun()
}

// Custom interactive surface to catch clicks across the entire video viewport
type interactiveTapArea struct {
	widget.BaseWidget
	onTap    func()
	controls *overlayControls
}

func newInteractiveTapArea(onTap func(), controls *overlayControls) *interactiveTapArea {
	t := &interactiveTapArea{onTap: onTap, controls: controls}
	t.ExtendBaseWidget(t)
	return t
}

func (t *interactiveTapArea) CreateRenderer() fyne.WidgetRenderer {
	rect := canvas.NewRectangle(color.Transparent)
	return widget.NewSimpleRenderer(rect)
}

func (t *interactiveTapArea) Tapped(*fyne.PointEvent) {
	if t.controls != nil {
		t.controls.resetHideTimer()
	}
	if t.onTap != nil {
		t.onTap()
	}
}

func (t *interactiveTapArea) MouseIn(*desktop.MouseEvent) {
	if t.controls != nil {
		t.controls.resetHideTimer()
	}
}

func (t *interactiveTapArea) MouseOut() {}

func (t *interactiveTapArea) MouseMoved(*desktop.MouseEvent) {
	if t.controls != nil {
		t.controls.resetHideTimer()
	}
}

func copyToTemp(r io.Reader) (string, error) {
	tmp, err := os.CreateTemp("", "fyne-av-*.mp4")
	if err != nil {
		return "", err
	}
	_, err = io.Copy(tmp, r)
	tmp.Close()
	if err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	return tmp.Name(), nil
}

type overlayControls struct {
	win         fyne.Window
	container   fyne.CanvasObject
	controlsBox *fyne.Container
	ctrl        *av.Controller
	playBtn     *widget.Button
	progress    *widget.Slider
	timeLabel   *widget.Label
	volBtn      *widget.Button
	volSlider   *widget.Slider
	posTicker   *time.Ticker
	posStop     chan struct{}
	hideTimer   *time.Timer
	timerMutex  sync.Mutex
}

func newOverlayControls(win fyne.Window, ctrl *av.Controller) *overlayControls {
	oc := &overlayControls{win: win, ctrl: ctrl}

	createIconButton := func(icon fyne.Resource, action func()) *widget.Button {
		b := widget.NewButtonWithIcon("", icon, action)
		b.Importance = widget.LowImportance // Removes native button background rendering
		return b
	}

	oc.playBtn = createIconButton(theme.MediaPlayIcon(), func() {
		if oc.ctrl == nil {
			return
		}
		if oc.ctrl.State() == av.StatePlaying {
			oc.ctrl.Pause()
		} else {
			oc.ctrl.Play()
		}
	})

	oc.progress = widget.NewSlider(0, 1)
	oc.progress.Step = 0.001
	oc.progress.OnChanged = func(v float64) {
		oc.resetHideTimer()
		if oc.ctrl == nil {
			return
		}
		dur := oc.ctrl.Duration()
		if dur > 0 {
			oc.ctrl.Seek(time.Duration(v * float64(dur)))
		}
	}

	oc.timeLabel = widget.NewLabel("0:00 / 0:00")

	oc.volBtn = createIconButton(theme.VolumeUpIcon(), func() {
		if oc.ctrl == nil {
			return
		}
		oc.ctrl.SetMuted(!oc.ctrl.Muted())
		oc.updateVolIcon()
	})

	// Volume Slider configured for smooth dragging in 0.05 steps
	oc.volSlider = widget.NewSlider(0, 1)
	oc.volSlider.Step = 0.05
	oc.volSlider.Value = 1.0
	oc.volSlider.OnChanged = func(v float64) {
		oc.resetHideTimer()
		if oc.ctrl != nil {
			oc.ctrl.SetVolume(v)
			if oc.ctrl.Muted() && v > 0 {
				oc.ctrl.SetMuted(false)
			}
			oc.updateVolIcon()
		}
	}

	// Transparent container backgrounds
	pillColor := color.Transparent
	const controlHeight float32 = 40.0
	const pillRadius float32 = controlHeight / 2.0 // Radius maintained for geometry consistency

	// 1. PLAY/PAUSE CONTAINER (Transparent)
	playBg := canvas.NewRectangle(pillColor)
	playBg.CornerRadius = pillRadius
	playGroup := container.NewGridWrap(
		fyne.NewSize(controlHeight, controlHeight),
		container.NewStack(playBg, oc.playBtn),
	)

	// 2. SEEK GROUP CONTAINER (Transparent)
	seekBg := canvas.NewRectangle(pillColor)
	seekBg.CornerRadius = pillRadius
	seekContent := container.NewBorder(nil, nil, oc.timeLabel, nil, oc.progress)
	seekPill := container.NewStack(seekBg, container.NewPadded(seekContent))

	// 3. VOLUME GROUP CONTAINER (Transparent)
	volBg := canvas.NewRectangle(pillColor)
	volBg.CornerRadius = pillRadius

	volBtnBg := canvas.NewRectangle(color.Transparent)
	volBtnCircle := container.NewGridWrap(
		fyne.NewSize(32, 32),
		container.NewStack(volBtnBg, oc.volBtn),
	)

	volContent := container.NewBorder(nil, nil, volBtnCircle, nil, oc.volSlider)
	volPill := container.NewStack(volBg, container.NewPadded(volContent))
	volumeContainer := container.NewGridWrap(fyne.NewSize(160, controlHeight), volPill)

	// Overlay surface remains transparent
	bg := canvas.NewRectangle(color.Transparent)

	// Single bottom row layout
	singleRowControls := container.NewBorder(
		nil, nil,
		playGroup,
		volumeContainer,
		seekPill,
	)

	oc.controlsBox = container.NewStack(bg, container.NewPadded(singleRowControls))

	// Anchor controls to bottom of video frame
	oc.container = container.NewBorder(nil, oc.controlsBox, nil, nil)

	if ctrl != nil {
		oc.startTicker()
		oc.updatePlayIcon()
	}

	oc.resetHideTimer()
	return oc
}

func (oc *overlayControls) resetHideTimer() {
	oc.timerMutex.Lock()
	defer oc.timerMutex.Unlock()

	if !oc.controlsBox.Visible() {
		fyne.Do(func() {
			oc.controlsBox.Show()
		})
	}

	if oc.hideTimer != nil {
		oc.hideTimer.Stop()
	}

	oc.hideTimer = time.AfterFunc(2500*time.Millisecond, func() {
		fyne.Do(func() {
			if oc.ctrl != nil && oc.ctrl.State() == av.StatePlaying {
				oc.controlsBox.Hide()
			}
		})
	})
}

func (oc *overlayControls) updatePlayIcon() {
	if oc.ctrl == nil {
		oc.playBtn.SetIcon(theme.MediaPlayIcon())
		return
	}
	switch oc.ctrl.State() {
	case av.StatePlaying:
		oc.playBtn.SetIcon(theme.MediaPauseIcon())
	default:
		oc.playBtn.SetIcon(theme.MediaPlayIcon())
	}
}

func (oc *overlayControls) updateVolIcon() {
	if oc.ctrl == nil {
		return
	}
	if oc.ctrl.Muted() {
		oc.volBtn.SetIcon(theme.VolumeMuteIcon())
	} else {
		oc.volBtn.SetIcon(theme.VolumeUpIcon())
	}
}

func (oc *overlayControls) startTicker() {
	if oc.posTicker != nil {
		oc.posTicker.Stop()
	}
	if oc.posStop != nil {
		close(oc.posStop)
	}
	oc.posStop = make(chan struct{})
	oc.posTicker = time.NewTicker(500 * time.Millisecond)

	go func() {
		for {
			select {
			case <-oc.posStop:
				return
			case <-oc.posTicker.C:
				if oc.ctrl == nil {
					return
				}
				pos := oc.ctrl.Position()
				dur := oc.ctrl.Duration().Milliseconds()
				fyne.Do(func() {
					oc.timeLabel.SetText(fmt.Sprintf("%s / %s", formatTime(pos), formatTime(dur)))
					if dur > 0 {
						oc.progress.OnChanged = nil
						oc.progress.SetValue(float64(pos) / float64(dur))
						oc.progress.OnChanged = func(v float64) {
							oc.resetHideTimer()
							if oc.ctrl == nil {
								return
							}
							d := oc.ctrl.Duration()
							if d > 0 {
								oc.ctrl.Seek(time.Duration(v * float64(d)))
							}
						}
					}
					oc.updatePlayIcon()
				})
			}
		}
	}()
}

func (oc *overlayControls) stopTicker() {
	if oc.posTicker != nil {
		oc.posTicker.Stop()
		oc.posTicker = nil
	}
	if oc.posStop != nil {
		close(oc.posStop)
		oc.posStop = nil
	}
}

func formatTime(ms int64) string {
	if ms < 0 {
		ms = 0
	}
	sec := ms / 1000
	min := sec / 60
	sec = sec % 60
	return fmt.Sprintf("%d:%02d", min, sec)
}

func openMedia(w fyne.Window, path string, ctrlPtr **av.Controller, vpPtr **av.VideoPlayer, ocPtr **overlayControls) {
	if *ctrlPtr != nil {
		(*ctrlPtr).Stop()
		(*ctrlPtr).Close()
	}

	c, err := av.NewController(path, av.WithMode(av.ModeAuto))
	if err != nil {
		dialog.ShowError(fmt.Errorf("failed to open media: %w", err), w)
		return
	}
	*ctrlPtr = c

	if *ocPtr != nil {
		(*ocPtr).stopTicker()
	}

	player := av.NewVideoPlayer(c)
	player.SetAspectRatio(av.Aspect16_9)
	*vpPtr = player

	newOC := newOverlayControls(w, c)
	*ocPtr = newOC

	topOpenBtn := widget.NewButtonWithIcon("Open File", theme.FolderOpenIcon(), func() {
		dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
			if err != nil || reader == nil {
				return
			}
			go func() {
				tmpPath, copyErr := copyToTemp(reader)
				reader.Close()
				if copyErr != nil {
					fyne.Do(func() {
						dialog.ShowError(fmt.Errorf("failed to copy file: %w", copyErr), w)
					})
					return
				}
				fyne.Do(func() {
					openMedia(w, tmpPath, ctrlPtr, vpPtr, ocPtr)
				})
			}()
		}, w).Show()
	})
	topBar := container.NewHBox(topOpenBtn)

	tapOverlay := newInteractiveTapArea(func() {
		if c.State() == av.StatePlaying {
			c.Pause()
		} else {
			c.Play()
		}
	}, newOC)

	videoArea := container.NewStack(player, tapOverlay, newOC.container)
	content := container.NewBorder(topBar, nil, nil, nil, videoArea)
	w.SetContent(content)

	c.Play()
}